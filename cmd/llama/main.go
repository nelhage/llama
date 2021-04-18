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
	"io"
	"log"
	"os"
	"runtime"
	"runtime/pprof"
	"strings"

	"github.com/google/subcommands"
	"github.com/klauspost/compress/zstd"
	"github.com/nelhage/llama/cmd/internal/cli"
	"github.com/nelhage/llama/cmd/llama/internal/bootstrap"
	"github.com/nelhage/llama/cmd/llama/internal/function"
	"github.com/nelhage/llama/cmd/llama/internal/trace"
	"github.com/nelhage/llama/tracing"
)

func main() {
	subcommands.Register(subcommands.HelpCommand(), "")
	subcommands.Register(subcommands.FlagsCommand(), "")

	subcommands.Register(&bootstrap.BootstrapCommand{}, "config")
	subcommands.Register(&ConfigCommand{}, "config")
	subcommands.Register(&function.UpdateFunctionCommand{}, "config")

	subcommands.Register(&InvokeCommand{}, "")
	subcommands.Register(&XargsCommand{}, "")
	subcommands.Register(&DaemonCommand{}, "")

	subcommands.Register(&StoreCommand{}, "internals")
	subcommands.Register(&GetCommand{}, "internals")
	subcommands.Register(&trace.TraceCommand{}, "tracing")
	subcommands.Register(&MultigetCommand{}, "internals")

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
	var trace string
	var cpuProfile, memProfile string
	flag.StringVar(&regionOverride, "region", "", "AWS region")
	flag.StringVar(&storeOverride, "store", "", "Path to the llama object store. s3://BUCKET/PATH")
	flag.BoolVar(&debugAWS, "debug-aws", false, "Log all AWS requests/responses")
	flag.IntVar(&storeConcurrency, "s3-concurrency", defaultStoreConcurrency, "Maximum concurrent S3 uploads/downloads")
	flag.StringVar(&trace, "trace", "", "Write tracing data to file")
	flag.StringVar(&cpuProfile, "cpu-profile", "", "Write CPU profile to file")
	flag.StringVar(&memProfile, "mem-profile", "", "Write memory profile to file")

	flag.Parse()

	if cpuProfile != "" {
		f, err := os.Create(cpuProfile)
		if err != nil {
			log.Fatal("could not create CPU profile: ", err)
		}
		defer f.Close() // error handling omitted for example
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatal("could not start CPU profile: ", err)
		}
		defer pprof.StopCPUProfile()
	}
	if memProfile != "" {
		defer func() {
			f, err := os.Create(memProfile)
			if err != nil {
				log.Fatal("could not create memory profile: ", err)
			}
			defer f.Close() // error handling omitted for example
			runtime.GC()    // get up-to-date statistics
			if err := pprof.WriteHeapProfile(f); err != nil {
				log.Fatal("could not write memory profile: ", err)
			}
		}()
	}

	if trace != "" {
		fh, err := os.Create(trace)
		if err != nil {
			log.Fatalf("trace: %s", err.Error())
		}
		var w io.Writer = fh
		if strings.HasSuffix(trace, ".zstd") || strings.HasSuffix(trace, ".zst") {
			zw, err := zstd.NewWriter(fh,
				zstd.WithEncoderLevel(zstd.SpeedFastest),
			)
			if err != nil {
				log.Fatalf("trace: %s", err.Error())
			}
			w = zw
			defer fh.Close()
		}
		var wt *tracing.WriterTracer
		ctx, wt = tracing.WithWriterTracer(ctx, w)
		defer wt.Close()
	}

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
