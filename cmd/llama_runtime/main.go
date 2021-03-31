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
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws/session"

	"github.com/nelhage/llama/store"
	"github.com/nelhage/llama/store/s3store"
)

const DiskCacheLimit = 100 * 1024 * 1024

func initStore() (store.Store, error) {
	session, err := session.NewSession()
	if err != nil {
		return nil, err
	}
	url := os.Getenv("LLAMA_OBJECT_STORE")
	if url == "" {
		return nil, errors.New("Could not read llama s3 bucket from LLAMA_OBJECT_STORE")
	}
	cacheDir, err := ioutil.TempDir("", "llama.cache.*")
	if err != nil {
		return nil, err
	}
	opts := s3store.Options{
		DiskCachePath:  cacheDir,
		DiskCacheBytes: DiskCacheLimit,
	}
	s3, err := s3store.FromSessionAndOptions(session, url, opts)
	if err != nil {
		return nil, err
	}

	return s3, nil
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

	cmdline := computeCmdline(os.Args[1:])

	var workerId [8]byte
	if _, err := rand.Reader.Read(workerId[:]); err != nil {
		log.Fatalf("gen ID: %s", err.Error())
	}

	runtime := Runtime{
		store:    store,
		cmdline:  cmdline,
		workerId: hex.EncodeToString(workerId[:]),
	}

	lambda.StartWithContext(ctx, runtime.RunOne)
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
