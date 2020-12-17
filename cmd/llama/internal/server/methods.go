// Copyright 2020 Nelson Elhage
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package server

import (
	"context"
	"fmt"
	"os"
	"path"

	"github.com/nelhage/llama/daemon"
	"github.com/nelhage/llama/llama"
	"github.com/nelhage/llama/protocol"
)

func (d *Daemon) Ping(in daemon.PingArgs, reply *daemon.PingReply) error {
	*reply = daemon.PingReply{
		ServerPid: os.Getpid(),
	}
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
