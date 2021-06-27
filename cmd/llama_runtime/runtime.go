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

// +build llama.runtime

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/golang/snappy"
	"github.com/nelhage/llama/protocol"
	"github.com/nelhage/llama/protocol/files"
	"github.com/nelhage/llama/store"
	"github.com/nelhage/llama/tracing"
)

type Runtime struct {
	store    store.Store
	cmdline  []string
	jobCount int
	workerId string
}

type ParsedJob struct {
	Root  string
	Args  []string
	Stdin []byte
}

func (p *ParsedJob) Cleanup() error {
	return os.RemoveAll(p.Root)
}

func (p *ParsedJob) TempPath(name string) (string, error) {
	out := path.Join(p.Root, "tmp", name)
	if err := os.MkdirAll(path.Dir(out), 0755); err != nil {
		return "", err
	}
	return out, nil
}

const MaxInlineSpans = 100

func (r *Runtime) RunOne(ctx context.Context, job *protocol.InvocationSpec) (*protocol.InvocationResponse, error) {
	start := time.Now()

	var tracer *tracing.MemoryTracer
	var resp *protocol.InvocationResponse
	var err error

	r.jobCount += 1

	defer func() {
		if resp == nil {
			return
		}
		r.store.FetchAWSUsage(&resp.Usage.S3)
		mem, _ := strconv.ParseUint(os.Getenv("AWS_LAMBDA_FUNCTION_MEMORY_SIZE"), 10, 64)
		resp.Usage.Lambda.Millis = uint64((time.Since(start) + 3*time.Millisecond/2 - 1).Milliseconds())
		resp.Usage.Lambda.MB_Millis = resp.Usage.Lambda.Millis * mem
	}()

	if job.Trace != nil {
		var span *tracing.SpanBuilder
		topctx := ctx
		tracer = tracing.NewMemoryTracer(ctx)
		ctx = tracing.WithTracer(ctx, tracer)
		ctx, span = tracing.StartSpanInTrace(
			ctx, "runtime.Execute",
			job.Trace.TraceId,
			job.Trace.ParentId,
		)
		span.AddField("job_count", r.jobCount)
		span.AddField("worker_id", r.workerId)
		defer func() {
			span.End()
			if resp == nil {
				return
			}
			spans := tracer.Close()
			if len(spans) < MaxInlineSpans {
				resp.InlineSpans = spans
			} else {
				spandata, err := json.Marshal(spans)
				if err == nil {
					compressed := snappy.Encode(nil, spandata)
					// We have to use topctx so we
					// don't try to log spans to
					// the tracer we just
					// closed. This does mean we
					// won't see this upload in
					// tracing, but doing that
					// would involve an entire
					// additional layer of
					// complexity...
					resp.Spans, err = files.NewBlob(topctx, r.store, compressed)
				}
				if err != nil {
					resp.Spans = &protocol.Blob{Err: err.Error()}
				}
			}
		}()
	}

	resp, err = r.executeJob(ctx, job)

	return resp, err
}

func (r *Runtime) executeJob(ctx context.Context, job *protocol.InvocationSpec) (*protocol.InvocationResponse, error) {
	t_start := time.Now()
	parsed, err := r.parseJob(ctx, job)
	if err != nil {
		return nil, err
	}
	defer parsed.Cleanup()

	if err := os.MkdirAll(parsed.Root, 0755); err != nil {
		return nil, err
	}

	if len(parsed.Args) == 0 {
		return nil, errors.New("No arguments provided")
	}

	exe := parsed.Args[0]
	if strings.ContainsRune(exe, '/') {
		// Use as-is. Will be interpreted relative to the root
	} else {
		exe, err = exec.LookPath(exe)

		if err != nil {
			return nil, fmt.Errorf("resolving %q: %s", parsed.Args[0], err.Error())
		}
	}

	cmd := exec.Cmd{
		Path: exe,
		Dir:  parsed.Root,
		Args: parsed.Args,
	}
	if parsed.Stdin != nil {
		cmd.Stdin = bytes.NewReader(parsed.Stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &stdout

	log.Printf("starting command: %v\n", cmd.Args)

	t_exec := time.Now()

	{
		_, span := tracing.StartSpan(ctx, "exec")
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("starting command: %q", err)
		}
		cmd.Wait()
		span.End()
	}
	t_wait := time.Now()

	resp := protocol.InvocationResponse{
		ExitStatus: cmd.ProcessState.ExitCode(),
	}

	{
		ctx, span := tracing.StartSpan(ctx, "upload")
		resp.Stdout, err = files.NewBlob(ctx, r.store, stdout.Bytes())
		if err != nil {
			resp.Stdout = &protocol.Blob{Err: err.Error()}
		}
		resp.Stderr, err = files.NewBlob(ctx, r.store, stderr.Bytes())
		if err != nil {
			resp.Stderr = &protocol.Blob{Err: err.Error()}
		}
		for _, out := range job.Outputs {
			file, err := files.ReadFile(ctx, r.store, path.Join(parsed.Root, out))
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				file = &protocol.File{
					Blob: protocol.Blob{
						Err: err.Error(),
					},
				}
			}
			resp.Outputs = append(resp.Outputs, protocol.FileAndPath{Path: out, File: *file})
		}
		span.End()
	}
	t_done := time.Now()

	resp.Times.ColdStart = r.jobCount == 1
	resp.Times.Fetch = t_exec.Sub(t_start)
	resp.Times.Exec = t_wait.Sub(t_exec)
	resp.Times.Upload = t_done.Sub(t_wait)
	resp.Times.E2E = t_done.Sub(t_start)

	return &resp, nil
}

func (r *Runtime) parseJob(ctx context.Context, spec *protocol.InvocationSpec) (*ParsedJob, error) {

	var err error
	temp, err := ioutil.TempDir("", "llama.*")
	if err != nil {
		return nil, err
	}
	job := ParsedJob{
		Root: temp,
		Args: r.cmdline,
	}

	job.Args = append(job.Args, spec.Args...)

	var gets []store.GetRequest

	if spec.Stdin != nil {
		gets = files.AppendGet(gets, spec.Stdin)
	}
	for i, file := range spec.Files {
		spec.Files[i].Path = path.Join(job.Root, file.Path)
		if err := os.MkdirAll(path.Dir(spec.Files[i].Path), 0755); err != nil {
			return nil, err
		}
		gets = files.AppendGet(gets, &file.Blob)
	}
	r.store.GetObjects(ctx, gets)

	if spec.Stdin != nil {
		var data []byte
		var err error
		data, err, gets = files.ReadBlob(spec.Stdin, gets)
		if err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		job.Stdin = data
	}

	for _, f := range spec.Files {
		err, gets = files.FetchFile(&f.File, f.Path, gets)
		if err != nil {
			return nil, err
		}
	}

	for _, f := range spec.Outputs {
		if err := os.MkdirAll(path.Join(job.Root, path.Dir(f)), 0755); err != nil {
			return nil, fmt.Errorf("creating output directory for %q: %s", f, err)
		}
	}
	return &job, nil
}
