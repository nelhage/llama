package daemon

import (
	"context"
	"fmt"
	"path"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/nelhage/llama/files"
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

type PingArgs struct{}
type PingReply struct{}

func (d *Daemon) Ping(in PingArgs, reply *PingReply) error {
	*reply = PingReply{}
	return nil
}

type ShutdownArgs struct{}
type ShutdownReply struct{}

func (d *Daemon) Shutdown(in ShutdownArgs, out *ShutdownReply) error {
	d.shutdown()
	*out = ShutdownReply{}
	return nil
}

type InvokeWithFilesArgs struct {
	Function   string
	ReturnLogs bool
	Args       []string
	Stdin      []byte
	Files      files.List
	Outputs    files.List
}

type InvokeWithFilesReply struct {
	InvokeErr  string
	ExitStatus int
	Stdout     []byte
	Stderr     []byte
	Logs       []byte
}

func (d *Daemon) InvokeWithFiles(in *InvokeWithFilesArgs, out *InvokeWithFilesReply) error {
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
	*out = InvokeWithFilesReply{
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
