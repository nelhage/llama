package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"os/exec"
	"path"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/gofrs/flock"
	"github.com/nelhage/llama/daemon"
	"github.com/nelhage/llama/llama"
	"github.com/nelhage/llama/protocol"
	"github.com/nelhage/llama/store"
)

type Daemon struct {
	shutdown context.CancelFunc
	store    store.Store
	session  *session.Session
	lambda   *lambda.Lambda
}

func (d *Daemon) Ping(in daemon.PingArgs, reply *daemon.PingReply) error {
	*reply = daemon.PingReply{}
	return nil
}

func (d *Daemon) Shutdown(in daemon.ShutdownArgs, out *daemon.ShutdownReply) error {
	d.shutdown()
	*out = daemon.ShutdownReply{}
	return nil
}

func (d *Daemon) InvokeWithFiles(in *daemon.InvokeWithFilesArgs, out *daemon.InvokeWithFilesReply) error {
	ctx := context.Background()

	for _, f := range in.Files {
		if f.Local.Path != "" && !path.IsAbs(f.Local.Path) {
			return fmt.Errorf("must pass absolute path: %s", f.Local.Path)
		}
	}

	for _, f := range in.Outputs {
		if f.Local.Path == "" {
			return fmt.Errorf("file %q: must have local path", f.Remote)
		}
		if !path.IsAbs(f.Local.Path) {
			return fmt.Errorf("must pass absolute path: %s", f.Local.Path)
		}
	}

	args := llama.InvokeArgs{
		Function:   in.Function,
		ReturnLogs: in.ReturnLogs,
		Spec: protocol.InvocationSpec{
			Args: in.Args,
		},
	}

	var err error
	args.Spec.Files, err = in.Files.Upload(ctx, d.store, nil)
	if err != nil {
		return err
	}
	if in.Stdin != nil {
		args.Spec.Stdin, err = protocol.NewBlob(ctx, d.store, in.Stdin)
		if err != nil {
			return err
		}
	}
	for _, out := range in.Outputs {
		args.Spec.Outputs = append(args.Spec.Outputs, out.Remote)
	}

	repl, invokeErr := llama.Invoke(ctx, d.lambda, &args)
	if invokeErr != nil && repl == nil {
		return invokeErr
	}

	if repl.Response.Outputs != nil {
		fetchErr := in.Outputs.Fetch(ctx, d.store, repl.Response.Outputs)
		if invokeErr == nil {
			invokeErr = fetchErr
		}
	}
	*out = daemon.InvokeWithFilesReply{
		Logs:       repl.Logs,
		ExitStatus: repl.Response.ExitStatus,
	}
	if invokeErr != nil {
		out.InvokeErr = invokeErr.Error()
	}

	if repl.Response.Stdout != nil {
		out.Stdout, _ = repl.Response.Stdout.Read(ctx, d.store)
	}

	if repl.Response.Stderr != nil {
		out.Stderr, _ = repl.Response.Stderr.Read(ctx, d.store)
	}

	return nil
}

var ErrAlreadyRunning = errors.New("daemon already running")

type StartArgs struct {
	Path        string
	Store       store.Store
	Session     *session.Session
	IdleTimeout time.Duration
}

func Start(ctx context.Context, args *StartArgs) error {
	if err := os.MkdirAll(path.Dir(args.Path), 0700); err != nil {
		return err
	}

	lk := flock.New(args.Path + ".lock")
	ok, err := lk.TryLock()
	if err != nil {
		return err
	}
	if !ok {
		return ErrAlreadyRunning
	}
	defer lk.Unlock()

	// Unlink the socket if it already exists. We have the
	// exclusive lock, so we know no one is listening.
	os.Remove(args.Path)
	listener, err := net.Listen("unix", args.Path)

	if err != nil {
		return err
	}

	srvCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	daemon := Daemon{
		shutdown: cancel,
		store:    args.Store,
		session:  args.Session,
		lambda:   lambda.New(args.Session),
	}

	extend := make(chan struct{})
	go func() {
		waitForIdle(srvCtx, extend, args.IdleTimeout)
		cancel()
	}()

	var httpSrv http.Server
	var rpcSrv rpc.Server
	rpcSrv.Register(&daemon)
	httpSrv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		extend <- struct{}{}
		rpcSrv.ServeHTTP(w, r)
	})
	go func() {
		httpSrv.Serve(listener)
	}()
	<-srvCtx.Done()

	httpSrv.Shutdown(ctx)
	return nil
}

func DialWithAutostart(ctx context.Context, path string) (*daemon.Client, error) {
	cl, err := daemon.Dial(ctx, path)
	if err == nil {
		return cl, nil
	}
	cmd := exec.Command("/proc/self/exe", "daemon", "-autostart", "-path", path)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	exitStatus := make(chan error)
	connected := make(chan *daemon.Client)
	shutdown := make(chan struct{})
	go func() {
		exitStatus <- cmd.Wait()
	}()
	go func() {
		for {
			cl, err := daemon.Dial(ctx, path)
			if err == nil {
				connected <- cl
				return
			}
			select {
			case <-shutdown:
				return
			case <-time.After(10 * time.Millisecond):
				// Try again
			}
		}
	}()
	select {
	case cl = <-connected:
		return cl, nil
	case err := <-exitStatus:
		close(shutdown)
		return nil, fmt.Errorf("Starting server: %s", err.Error())
	}
}

func waitForIdle(srvCtx context.Context, extend chan struct{}, timeout time.Duration) {
	var timer *time.Timer
	var expire <-chan time.Time
	if timeout != 0 {
		timer = time.NewTimer(timeout)
		expire = timer.C
	}
loop:
	for {
		select {
		case <-srvCtx.Done():
			break loop
		case <-expire:
			break loop
		case <-extend:
			if timer != nil {
				if !timer.Stop() {
					<-timer.C
				}
				timer.Reset(timeout)
				expire = timer.C
			}
		}
	}
	if timer != nil {
		timer.Stop()
	}
}
