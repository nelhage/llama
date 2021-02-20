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
	"fmt"
	"log"
	"os"
	"path"
	"sync/atomic"
	"time"

	"github.com/nelhage/llama/daemon"
	"github.com/nelhage/llama/llama"
	"github.com/nelhage/llama/protocol"
	"github.com/nelhage/llama/tracing"
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
	ctx := d.ctx
	ctx, sb := tracing.StartPropagatedSpan(ctx, "InvokeWithFiles", in.Trace)
	defer sb.End()
	sb.AddField("function", in.Function)

	atomic.AddUint64(&d.stats.Invocations, 1)
	inflight := atomic.AddUint64(&d.stats.InFlight, 1)
	sb.AddField("inflight", float64(inflight))
	if len(in.Outputs) > 0 && in.Outputs[0].Local.Path != "" {
		sb.AddField("output", in.Outputs[0].Local.Path)
	}
	if len(in.Files) > 0 && in.Files[0].Local.Path != "" {
		sb.AddField("file", in.Files[0].Local.Path)
	}
	defer atomic.AddUint64(&d.stats.InFlight, ^uint64(0))
	for {
		oldmax := atomic.LoadUint64(&d.stats.MaxInFlight)
		if inflight <= oldmax {
			break
		}
		if atomic.CompareAndSwapUint64(&d.stats.MaxInFlight, oldmax, inflight) {
			break
		}
	}

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

	t_start := time.Now()

	{
		ctx, sb := tracing.StartSpan(ctx, "upload")
		var err error
		args.Spec.Files, err = in.Files.Upload(ctx, d.store, nil)
		if err != nil {
			sb.AddField("error", fmt.Sprintf("upload: %s", err.Error()))
			return err
		}
		if in.Stdin != nil {
			args.Spec.Stdin, err = protocol.NewBlob(ctx, d.store, in.Stdin)
			if err != nil {
				sb.AddField("error", fmt.Sprintf("stdin: %s", err.Error()))
				return err
			}
		}
		for _, out := range in.Outputs {
			args.Spec.Outputs = append(args.Spec.Outputs, out.Remote)
		}
		sb.End()
	}

	t_invoke := time.Now()

	repl, invokeErr := llama.Invoke(ctx, d.lambda, d.store, &args)
	if invokeErr != nil {
		sb.AddField("error", fmt.Sprintf("invoke: %s", invokeErr.Error()))
		if _, ok := invokeErr.(*llama.ErrorReturn); ok {
			atomic.AddUint64(&d.stats.FunctionErrors, 1)
		} else {
			atomic.AddUint64(&d.stats.OtherErrors, 1)
		}
	}

	if invokeErr != nil && repl == nil {
		return invokeErr
	}

	t_fetch := time.Now()

	atomic.AddUint64(&d.stats.ExitStatuses[repl.Response.ExitStatus&0xff], 1)

	{
		if repl.Response.Outputs != nil {
			ctx, fetch := tracing.StartSpan(ctx, "fetch")
			fetchList, extra := in.Outputs.TransformToLocal(ctx, repl.Response.Outputs)
			for _, out := range extra {
				log.Printf("Remote returned unexpected output: %s", out.Path)
			}

			fetchErr := fetchList.Fetch(ctx, d.store)
			if fetchErr != nil {
				sb.AddField("error", fmt.Sprintf("fetch: %s", fetchErr.Error()))
			}
			if invokeErr == nil {
				invokeErr = fetchErr
			}
			fetch.End()
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

	t_end := time.Now()

	out.Timing.Remote = repl.Response.Times
	out.Timing.Upload = t_invoke.Sub(t_start)
	out.Timing.Invoke = t_fetch.Sub(t_invoke)
	out.Timing.Fetch = t_end.Sub(t_fetch)
	out.Timing.E2E = t_end.Sub(t_start)

	sb.AddField("upload_ms", out.Timing.Upload.Milliseconds())
	sb.AddField("invoke_ms", out.Timing.Invoke.Milliseconds())
	sb.AddField("fetch_ms", out.Timing.Fetch.Milliseconds())
	sb.AddField("e2e_ms", out.Timing.E2E.Milliseconds())

	return nil
}

func (d *Daemon) GetDaemonStats(in *daemon.StatsArgs, out *daemon.StatsReply) error {
	*out = daemon.StatsReply{
		// TODO: We should really read this a field-at-a-time
		// using `atomic.LoadUint64`, although I don't believe
		// that can make any difference on any platform I'm
		// aware of. In either case we won't get a consistent
		// snapshot of the entire stats struct. We could just
		// use a mutex, I guess.
		Stats: d.stats,
	}
	return nil
}

func (d *Daemon) TraceSpans(in *daemon.TraceSpansArgs, out *daemon.TraceSpansReply) error {
	tracing.SubmitAll(d.ctx, in.Spans)
	*out = daemon.TraceSpansReply{}
	return nil
}
