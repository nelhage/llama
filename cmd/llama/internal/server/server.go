package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"path"
	"syscall"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/lambda"
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
	Path    string
	Store   store.Store
	Session *session.Session
}

func Start(ctx context.Context, args *StartArgs) error {
	if err := os.MkdirAll(path.Dir(args.Path), 0700); err != nil {
		return err
	}
	listener, err := net.Listen("unix", args.Path)
	if err != nil && errors.Is(err, syscall.EADDRINUSE) {
		var client *daemon.Client
		// The socket exists. Is someone listening?
		client, err = daemon.Dial(ctx, args.Path)
		if err == nil {
			_, err = client.Ping(&daemon.PingArgs{})
			if err == nil {
				return ErrAlreadyRunning
			}
			return err
		}
		// TODO: be atomic (lockfile?) if multiple clients hit
		// this path at once.
		if err := os.Remove(args.Path); err != nil {
			return err
		}
		listener, err = net.Listen("unix", args.Path)
	}
	if err != nil {
		return err
	}

	srvCtx, cancel := context.WithCancel(ctx)

	daemon := Daemon{
		shutdown: cancel,
		store:    args.Store,
		session:  args.Session,
		lambda:   lambda.New(args.Session),
	}

	var httpSrv http.Server
	var rpcSrv rpc.Server
	rpcSrv.Register(&daemon)
	httpSrv.Handler = &rpcSrv
	go func() {
		httpSrv.Serve(listener)
	}()
	<-srvCtx.Done()

	httpSrv.Shutdown(ctx)
	return nil
}
