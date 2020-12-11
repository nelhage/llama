package daemon

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
	"github.com/nelhage/llama/store"
)

func SocketPath() string {
	if sock := os.Getenv("LLAMA_SOCKET"); sock != "" {
		return sock
	}
	if home := os.Getenv("HOME"); home != "" {
		return path.Join(home, ".llama", "llama.sock")
	}
	return fmt.Sprintf("/run/llama-%d/llama.sock", os.Getuid())
}

var ErrAlreadyRunning = errors.New("daemon already running")

func Start(ctx context.Context, store store.Store, sess *session.Session) error {
	sockPath := SocketPath()
	if err := os.MkdirAll(path.Dir(sockPath), 0700); err != nil {
		return err
	}
	listener, err := net.Listen("unix", sockPath)
	if err != nil && errors.Is(err, syscall.EADDRINUSE) {
		var client *Client
		// The socket exists. Is someone listening?
		client, err = Dial(ctx)
		if err == nil {
			_, err = client.Ping(&PingArgs{})
			if err == nil {
				return ErrAlreadyRunning
			}
			return err
		}
		// TODO: be atomic (lockfile?) if multiple clients hit
		// this path at once.
		if err := os.Remove(sockPath); err != nil {
			return err
		}
		listener, err = net.Listen("unix", sockPath)
	}
	if err != nil {
		return err
	}

	srvCtx, cancel := context.WithCancel(ctx)

	daemon := Daemon{
		shutdown: cancel,
		store:    store,
		session:  sess,
		lambda:   lambda.New(sess),
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

func Dial(_ context.Context) (*Client, error) {
	conn, err := rpc.DialHTTP("unix", SocketPath())
	if err != nil {
		return nil, err
	}
	return &Client{conn}, nil
}
