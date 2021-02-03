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
	"context"
	"flag"
	"log"
	"os"

	"github.com/google/subcommands"
	"github.com/nelhage/llama/cmd/internal/cli"
	"github.com/nelhage/llama/cmd/llama/internal/bootstrap"
)

func main() {
	subcommands.Register(subcommands.HelpCommand(), "")
	subcommands.Register(subcommands.FlagsCommand(), "")

	subcommands.Register(&bootstrap.BootstrapCommand{}, "config")
	subcommands.Register(&ConfigCommand{}, "config")

	subcommands.Register(&InvokeCommand{}, "")
	subcommands.Register(&XargsCommand{}, "")
	subcommands.Register(&DaemonCommand{}, "")

	subcommands.Register(&StoreCommand{}, "internals")
	subcommands.Register(&GetCommand{}, "internals")

	subcommands.ImportantFlag("region")

	ctx := context.Background()
	code := runLlama(ctx)
	os.Exit(code)
}

const defaultStoreConcurrency = 8

func runLlama(ctx context.Context) int {
	var regionOverride string
	var storeOverride string
	debugAWS := false
	var storeConcurrency int
	flag.StringVar(&regionOverride, "region", "", "AWS region")
	flag.StringVar(&storeOverride, "store", "", "Path to the llama object store. s3://BUCKET/PATH")
	flag.BoolVar(&debugAWS, "debug-aws", false, "Log all AWS requests/responses")
	flag.IntVar(&storeConcurrency, "s3-concurrency", defaultStoreConcurrency, "Maximum concurrent S3 uploads/downloads")

	flag.Parse()

	cfg, err := cli.ReadConfig(cli.ConfigPath())
	if err != nil {
		log.Fatalf("reading config file: %s", err.Error())
	}

	if storeOverride == "" {
		storeOverride = os.Getenv("LLAMA_OBJECT_STORE")
	}
	if storeOverride != "" {
		cfg.Store = storeOverride
	}
	if storeConcurrency != defaultStoreConcurrency || cfg.S3Concurrency == 0 {
		cfg.S3Concurrency = storeConcurrency
	}
	if regionOverride != "" {
		cfg.Region = regionOverride
	}
	cfg.DebugAWS = debugAWS

	var state cli.GlobalState
	state.Config = cfg

	ctx = cli.WithState(ctx, &state)

	if err != nil {
		log.Fatal(err.Error())
	}

	return int(subcommands.Execute(ctx))
}
