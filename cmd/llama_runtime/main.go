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
		return runOne(ctx, store, req)
	})
}

type ParsedJob struct {
	Temp    string
	Exe     string
	Root    string
	Args    []string
	Stdin   []byte
	Outputs map[string]string
}

func (p *ParsedJob) Cleanup() error {
	if p.Temp != "" {
		return os.RemoveAll(p.Temp)
	}
	return nil
}

func (p *ParsedJob) EnsureTemp() (string, error) {
	if p.Temp == "" {
		var err error
		p.Temp, err = ioutil.TempDir("", "llama.*")
		if err != nil {
			return "", err
		}
	}
	return p.Temp, nil
}

func (p *ParsedJob) TempPath(name string) (string, error) {
	tmp, err := p.EnsureTemp()
	if err != nil {
		return "", err
	}
	out := path.Join(tmp, name)
	if err := os.MkdirAll(path.Dir(out), 0755); err != nil {
		return "", err
	}
	return out, nil
}

func runOne(ctx context.Context, store store.Store, job *protocol.InvocationSpec) (*protocol.InvocationResponse, error) {
	parsed, err := parseJob(ctx, store, job)
	if err != nil {
		return nil, err
	}
	defer parsed.Cleanup()

	if err := os.MkdirAll(parsed.Root, 0755); err != nil {
		return nil, err
	}

	cmd := exec.Cmd{
		Path: parsed.Exe,
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
	if parsed.Outputs != nil {
		resp.Outputs = make(map[string]protocol.Blob, len(parsed.Outputs))
	}
	for out, path := range parsed.Outputs {
		var blob *protocol.Blob
		data, err := ioutil.ReadFile(path)
		if err == nil {
			blob, err = protocol.NewBlob(ctx, store, data)
		}
		if err != nil {
			blob = &protocol.Blob{Err: err.Error()}
		}
		resp.Outputs[out] = *blob
	}

	return &resp, nil
}

func parseJob(ctx context.Context, store store.Store, spec *protocol.InvocationSpec) (*ParsedJob, error) {
	root := os.Getenv("LAMBDA_TASK_ROOT")

	var err error
	job := ParsedJob{
		Root: root,
	}

	if len(os.Args) == 1 {
		// Running in packaged mode, pull our exe from the
		// environment
		handler := os.Getenv("_HANDLER")
		job.Exe = path.Join(root, handler)
		job.Args = []string{handler}
	} else {
		// We're running in a container. We'll have been
		// passed our command as our own ARGV
		job.Args = os.Args[1:]
		job.Exe = os.Args[1]
	}

	if spec.Stdin != nil {
		job.Stdin, err = spec.Stdin.Read(ctx, store)
		if err != nil {
			return nil, err
		}
	}

	for i, arg := range spec.Args {
		var s string
		if err := json.Unmarshal(arg, &s); err == nil {
			job.Args = append(job.Args, s)
			continue
		}
		var io protocol.Arg
		if err := json.Unmarshal(arg, &io); err != nil {
			return nil, fmt.Errorf("unable to interpret arg: %q", arg)
		}

		var argpath string

		if io.In != nil {
			argpath, err = job.TempPath(fmt.Sprintf("llama/arg-%d", i))
			if err != nil {
				return nil, err
			}
			data, err := io.In.Read(ctx, store)
			if err != nil {
				return nil, err
			}
			if err := ioutil.WriteFile(argpath, data, 0600); err != nil {
				return nil, err
			}
		}
		if io.Out != nil {
			if argpath == "" {
				argpath, err = job.TempPath(fmt.Sprintf("llama/out/%d_%s", i, *io.Out))
				if err != nil {
					return nil, err
				}
			}
			if job.Outputs == nil {
				job.Outputs = make(map[string]string)
			}
			job.Outputs[*io.Out] = argpath
		}
		job.Args = append(job.Args, argpath)
	}
	for path, file := range spec.Files {
		log.Printf("Writing file: %q", path)
		data, err := file.Read(ctx, store)
		if err != nil {
			return nil, err
		}
		path, err = job.TempPath(path)
		if err != nil {
			return nil, err
		}
		mode := file.Mode
		if mode == 0 {
			mode = 0644
		}
		if err := ioutil.WriteFile(path, data, mode); err != nil {
			return nil, err
		}
	}

	if job.Temp != "" {
		job.Root = job.Temp
	}

	return &job, nil
}
