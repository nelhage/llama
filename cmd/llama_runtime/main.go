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
	"net/http"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/golang/snappy"

	"github.com/nelhage/llama/protocol"
	"github.com/nelhage/llama/protocol/files"
	"github.com/nelhage/llama/store"
	"github.com/nelhage/llama/store/s3store"
	"github.com/nelhage/llama/tracing"
)

func initStore() (store.Store, error) {
	session, err := session.NewSession()
	if err != nil {
		return nil, err
	}
	url := os.Getenv("LLAMA_OBJECT_STORE")
	if url == "" {
		return nil, errors.New("Could not read llama s3 bucket from LLAMA_OBJECT_STORE")
	}
	return s3store.FromSession(session, url)
}

func main() {
	runtimeURI := os.Getenv("AWS_LAMBDA_RUNTIME_API")
	if runtimeURI == "" {
		log.Fatalf("could not read runtime API endpoint")
	}

	client := http.Client{}
	ctx := context.Background()

	store, err := initStore()
	if err != nil {
		log.Printf("initialization error: %s", err.Error())
		payload, _ := json.Marshal(struct {
			Error string `json:"error"`
		}{fmt.Sprintf("Unable to initialize store: %s", err.Error())})
		req, _ := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("http://%s/2018-06-01/runtime/init/error", runtimeURI), bytes.NewReader(payload))
		client.Do(req)
		os.Exit(1)
	}

	lambda.StartWithContext(ctx, func(ctx context.Context, req *protocol.InvocationSpec) (*protocol.InvocationResponse, error) {
		cmdline := computeCmdline(os.Args[1:])
		return runOne(ctx, store, cmdline, req)
	})
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

func computeCmdline(argv []string) []string {
	if handler := os.Getenv("_HANDLER"); handler != "" {
		// Running in packaged mode, pull our exe from the
		// environment
		return []string{handler}
	}

	// We're running in a container. We'll have been
	// passed our command as our own ARGV
	if len(argv) == 3 && argv[0] == "/bin/sh" && argv[1] == "-c" {
		// The Dockerfile used the [CMD "STRING"]
		// version of CMD, so it is being evaluated by
		// /bin/sh -c. In order to be able to append
		// arguments, we need to munge it a bit.
		return []string{
			"/bin/sh",
			"-c",
			fmt.Sprintf(`%s "$@"`, argv[2]),
			strings.SplitN(argv[2], " ", 2)[0],
		}
	}
	return argv
}

var jobs = 0

func runOne(ctx context.Context, store store.Store,
	cmdline []string,
	job *protocol.InvocationSpec) (*protocol.InvocationResponse, error) {

	var tracer *tracing.MemoryTracer
	var resp *protocol.InvocationResponse
	var err error

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
		defer func() {
			span.End()
			if resp == nil {
				return
			}
			spans := tracer.Close()
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
				resp.Spans, err = files.NewBlob(topctx, store, compressed)
			}
			if err != nil {
				resp.Spans = &protocol.Blob{Err: err.Error()}
			}
		}()
	}

	resp, err = executeJob(ctx, store, cmdline, job)

	return resp, err
}

func executeJob(ctx context.Context, store store.Store,
	cmdline []string,
	job *protocol.InvocationSpec) (*protocol.InvocationResponse, error) {

	jobs += 1

	t_start := time.Now()
	parsed, err := parseJob(ctx, store, cmdline, job)
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
		resp.Stdout, err = files.NewBlob(ctx, store, stdout.Bytes())
		if err != nil {
			resp.Stdout = &protocol.Blob{Err: err.Error()}
		}
		resp.Stderr, err = files.NewBlob(ctx, store, stderr.Bytes())
		if err != nil {
			resp.Stderr = &protocol.Blob{Err: err.Error()}
		}
		for _, out := range job.Outputs {
			file, err := files.ReadFile(ctx, store, path.Join(parsed.Root, out))
			if err != nil {
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

	resp.Times.ColdStart = jobs == 1
	resp.Times.Fetch = t_exec.Sub(t_start)
	resp.Times.Exec = t_wait.Sub(t_exec)
	resp.Times.Upload = t_done.Sub(t_wait)
	resp.Times.E2E = t_done.Sub(t_start)

	return &resp, nil
}

func parseJob(ctx context.Context,
	st store.Store,
	cmdline []string,
	spec *protocol.InvocationSpec) (*ParsedJob, error) {

	var err error
	temp, err := ioutil.TempDir("", "llama.*")
	if err != nil {
		return nil, err
	}
	job := ParsedJob{
		Root: temp,
		Args: cmdline,
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
	st.GetObjects(ctx, gets)

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
