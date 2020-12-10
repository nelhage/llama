// +build llama.runtime

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

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws/session"

	"github.com/nelhage/llama/protocol"
	"github.com/nelhage/llama/store"
	"github.com/nelhage/llama/store/s3store"
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

func runOne(ctx context.Context, store store.Store,
	cmdline []string,
	job *protocol.InvocationSpec) (*protocol.InvocationResponse, error) {

	parsed, err := parseJob(ctx, store, cmdline, job)
	if err != nil {
		return nil, err
	}
	defer parsed.Cleanup()

	if err := os.MkdirAll(parsed.Root, 0755); err != nil {
		return nil, err
	}

	var exe string
	exe, err = exec.LookPath(parsed.Args[0])

	if err != nil {
		return nil, fmt.Errorf("resolving %q: %s", parsed.Args[0], err.Error())
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

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting command: %q", err)
	}
	cmd.Wait()
	resp := protocol.InvocationResponse{
		ExitStatus: cmd.ProcessState.ExitCode(),
	}

	resp.Stdout, err = protocol.NewBlob(ctx, store, stdout.Bytes())
	if err != nil {
		resp.Stdout = &protocol.Blob{Err: err.Error()}
	}
	resp.Stderr, err = protocol.NewBlob(ctx, store, stderr.Bytes())
	if err != nil {
		resp.Stderr = &protocol.Blob{Err: err.Error()}
	}
	if job.Outputs != nil {
		resp.Outputs = make(map[string]protocol.File, len(job.Outputs))
	}
	for _, out := range job.Outputs {
		file, err := protocol.ReadFile(ctx, store, path.Join(parsed.Root, out))
		if err != nil {
			file = &protocol.File{
				Blob: protocol.Blob{
					Err: err.Error(),
				},
			}
		}
		resp.Outputs[out] = *file
	}

	return &resp, nil
}

func parseJob(ctx context.Context,
	store store.Store,
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

	if spec.Stdin != nil {
		job.Stdin, err = spec.Stdin.Read(ctx, store)
		if err != nil {
			return nil, err
		}
	}
	job.Args = append(job.Args, spec.Args...)

	for p, file := range spec.Files {
		data, err := file.Read(ctx, store)
		if err != nil {
			return nil, err
		}
		dest := path.Join(job.Root, p)
		if err := os.MkdirAll(path.Dir(dest), 0755); err != nil {
			return nil, err
		}
		mode := file.Mode
		if mode == 0 {
			mode = 0644
		}
		log.Printf("Writing file: %q", dest)
		if err := ioutil.WriteFile(dest, data, mode); err != nil {
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
