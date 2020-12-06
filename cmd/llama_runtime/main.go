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
	Exe     string
	Root    string
	Args    []string
	Stdin   []byte
	Outputs map[string]string
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

func computeCmdline(argv []string) ([]string, error) {
	var cmdline []string
	if len(argv) == 0 {
		// Running in packaged mode, pull our exe from the
		// environment
		cmdline = []string{os.Getenv("_HANDLER")}
	} else {
		// We're running in a container. We'll have been
		// passed our command as our own ARGV
		if len(argv) == 3 && argv[0] == "/bin/sh" && argv[1] == "-c" {
			// The Dockerfile used the [CMD "STRING"]
			// version of CMD, so it is being evaluated by
			// /bin/sh -c. In order to be able to append
			// arguments, we need to munge it a bit.
			cmdline = []string{
				"/bin/sh",
				"-c",
				fmt.Sprintf(`%s "$@"`, argv[2]),
				strings.SplitN(argv[2], " ", 2)[0],
			}
		} else {
			if exe, err := exec.LookPath(argv[0]); err != nil {
				return nil, fmt.Errorf("resolving %q: %s", argv[0], err.Error())
			} else {
				cmdline = append([]string{exe}, argv[1:]...)
			}
		}
	}
	return cmdline, nil
}

func runOne(ctx context.Context, store store.Store, job *protocol.InvocationSpec) (*protocol.InvocationResponse, error) {
	cmdline, err := computeCmdline(os.Args[1:])
	if err != nil {
		return nil, err
	}

	parsed, err := parseJob(ctx, store, cmdline, job)
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
		Exe:  cmdline[0],
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
			argpath, err = job.TempPath(fmt.Sprintf("arg-%d", i))
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
				argpath, err = job.TempPath(fmt.Sprintf("out/%d_%s", i, *io.Out))
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

	return &job, nil
}
