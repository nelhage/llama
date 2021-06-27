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
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nelhage/llama/daemon"
	"github.com/nelhage/llama/llama"
	"github.com/nelhage/llama/protocol"
	"github.com/nelhage/llama/protocol/files"
	"github.com/nelhage/llama/store"
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

	if in.DropSemaphore {
		d.releaseSem()
		defer d.acquireSem(ctx)
	}

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
		sb.AddField("files", len(in.Files))
		var err error
		args.Spec.Files, err = in.Files.Upload(ctx, d.store, nil)
		if err != nil {
			sb.AddField("error", fmt.Sprintf("upload: %s", err.Error()))
			return err
		}
		if in.Stdin != nil {
			args.Spec.Stdin, err = files.NewBlob(ctx, d.store, in.Stdin)
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

	atomic.AddUint64(&d.stats.Usage.Lambda.Requests, 1)
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
	atomic.AddUint64(&d.stats.Usage.Lambda.MB_Millis, repl.Response.Usage.Lambda.MB_Millis)
	atomic.AddUint64(&d.stats.Usage.Lambda.Millis, repl.Response.Usage.Lambda.Millis)
	atomic.AddUint64(&d.stats.Usage.RemoteS3.Read_Requests, repl.Response.Usage.S3.Read_Requests)
	atomic.AddUint64(&d.stats.Usage.RemoteS3.Write_Requests, repl.Response.Usage.S3.Write_Requests)
	atomic.AddUint64(&d.stats.Usage.RemoteS3.Xfer_In, repl.Response.Usage.S3.Xfer_In)
	atomic.AddUint64(&d.stats.Usage.RemoteS3.Xfer_Out, repl.Response.Usage.S3.Xfer_Out)

	var gets []store.GetRequest

	var fetchList, extra protocol.FileList
	if repl.Response.Outputs != nil {
		fetchList, extra = in.Outputs.TransformToLocal(ctx, repl.Response.Outputs)
		for _, out := range extra {
			log.Printf("Remote returned unexpected output: %s", out.Path)
		}
		for _, f := range fetchList {
			gets = files.AppendGet(gets, &f.Blob)
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
		gets = files.AppendGet(gets, repl.Response.Stdout)
	}

	if repl.Response.Stderr != nil {
		gets = files.AppendGet(gets, repl.Response.Stderr)
	}

	d.store.GetObjects(ctx, gets)

	for _, f := range fetchList {
		var err error
		err, gets = files.FetchFile(&f.File, f.Path, gets)
		if err != nil && out.InvokeErr == "" {
			out.InvokeErr = err.Error()
		}
	}

	if repl.Response.Stdout != nil {
		out.Stdout, _, gets = files.ReadBlob(repl.Response.Stdout, gets)
	}

	if repl.Response.Stderr != nil {
		out.Stderr, _, gets = files.ReadBlob(repl.Response.Stderr, gets)
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
	d.store.FetchAWSUsage(&d.stats.Usage.LocalS3)

	// TODO: We should really read this a field-at-a-time
	// using `atomic.LoadUint64`, although I don't believe
	// that can make any difference on any platform I'm
	// aware of. In either case we won't get a consistent
	// snapshot of the entire stats struct. We could just
	// use a mutex, I guess.
	stats := d.stats

	*out = daemon.StatsReply{
		Stats: stats,
	}
	if in.Reset {
		d.stats = daemon.Stats{}
	}
	return nil
}

func (d *Daemon) TraceSpans(in *daemon.TraceSpansArgs, out *daemon.TraceSpansReply) error {
	tracing.SubmitAll(d.ctx, in.Spans)
	*out = daemon.TraceSpansReply{}
	return nil
}

func (d *Daemon) GetCompilerIncludePath(in *daemon.GetCompilerIncludePathArgs, out *daemon.GetCompilerIncludePathReply) error {
	key := compilerAndLanguage{compiler: in.Compiler, language: in.Language}
	d.includePathCache.RLock()
	if ent, ok := d.includePathCache.paths[key]; ok {
		d.includePathCache.RUnlock()
		out.Paths = ent
		return nil
	}
	d.includePathCache.RUnlock()
	d.includePathCache.Lock()
	defer d.includePathCache.Unlock()

	if ent, ok := d.includePathCache.paths[key]; ok {
		out.Paths = ent
		return nil
	}

	paths, err := discoverDefaultSearchPath(in.Compiler, in.Language)
	if err != nil {
		return err
	}

	d.includePathCache.paths[key] = paths
	out.Paths = paths
	return nil
}

func discoverDefaultSearchPath(compiler string, lang string) ([]string, error) {
	var exe exec.Cmd
	exe.Path = compiler
	exe.Args = []string{compiler, "-Wp,-v", "-x", lang, "-E", "-"}
	var stderr bytes.Buffer
	exe.Stderr = &stderr

	if err := exe.Run(); err != nil {
		return nil, err
	}

	var paths []string
	for {
		line, err := stderr.ReadString('\n')
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(line, " /") {
			dir := strings.Trim(line, " \n")
			paths = append(paths, path.Clean(dir))
		}
	}
	return paths, nil
}
