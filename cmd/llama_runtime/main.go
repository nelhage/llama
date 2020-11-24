package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"strings"
)

func main() {
	runtimeURI := os.Getenv("AWS_LAMBDA_RUNTIME_API")
	if runtimeURI == "" {
		log.Fatalf("could not read runtime API endpoint")
	}

	client := http.Client{}
	ctx := context.Background()

	for {
		req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("http://%s/2018-06-01/runtime/invocation/next", runtimeURI), nil)
		if err != nil {
			log.Fatal("new request: ", err)
		}
		job, err := client.Do(req)
		if err != nil {
			log.Fatal("/next: ", err)
		}
		reqId := job.Header.Get("Lambda-Runtime-Aws-Request-Id")

		resp, err := runOne(job)
		var payload []byte
		if err == nil {
			payload, err = json.Marshal(resp)
		}
		if err != nil {
			errorPayload, _ := json.Marshal(struct {
				Error string `json:"error"`
			}{err.Error()})
			req, err = http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("http://%s/2018-06-01/runtime/invocation/%s/error", runtimeURI, reqId),
				bytes.NewReader(errorPayload))
			if err != nil {
				log.Fatal("build response: ", err)
			}
		} else {
			req, err = http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("http://%s/2018-06-01/runtime/invocation/%s/response", runtimeURI, reqId),
				bytes.NewReader(payload))
		}
		_, err = client.Do(req)
		if err != nil {
			log.Fatal("finishing request: ", err)
		}
	}
}

type InvocationSpec struct {
	Args  []string `json:"args"`
	Stdin string   `json:"stdin"`
}

type InvocationResponse struct {
	ExitStatus int    `json:"status"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
}

func runOne(job *http.Response) (interface{}, error) {
	handler := os.Getenv("_HANDLER")
	root := os.Getenv("LAMBDA_TASK_ROOT")

	var spec InvocationSpec

	body, err := ioutil.ReadAll(job.Body)
	if err != nil {
		return nil, fmt.Errorf("reading body: %w", err)
	}

	if err := json.Unmarshal(body, &spec); err != nil {
		return nil, fmt.Errorf("parsing body: %w", err)
	}

	cmd := exec.Cmd{
		Path: path.Join(root, handler),
		Dir:  root,
		Args: append([]string{handler}, spec.Args...),
	}
	if spec.Stdin != "" {
		cmd.Stdin = strings.NewReader(spec.Stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &stdout

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting command: %w", err)
	}
	cmd.Wait()
	resp := InvocationResponse{
		ExitStatus: cmd.ProcessState.ExitCode(),
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
	}

	return resp, nil
}
