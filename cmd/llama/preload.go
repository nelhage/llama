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
	"path/filepath"

	"github.com/google/subcommands"
	"github.com/nelhage/llama/cmd/internal/cli"
	"github.com/nelhage/llama/daemon"
	"github.com/nelhage/llama/daemon/server"
)

type PreloadCommand struct {
	pattern string
}

func (*PreloadCommand) Name() string     { return "preload" }
func (*PreloadCommand) Synopsis() string { return "Batch-upload files to the Llama store" }
func (*PreloadCommand) Usage() string {
	return `config PATH...
`
}

func (c *PreloadCommand) SetFlags(flags *flag.FlagSet) {
	flags.StringVar(&c.pattern, "pattern", `[.](c|cc|cxx|h|hpp|ipp)$`, "Regular expression defining files to preload")
}

func (c *PreloadCommand) Execute(ctx context.Context, flag *flag.FlagSet, _ ...interface{}) subcommands.ExitStatus {
	client, err := server.DialWithAutostart(ctx, cli.SocketPath())
	if err != nil {
		log.Printf("Unable to connect to daemon: %s", err.Error())
		return subcommands.ExitFailure
	}
	var args daemon.PreloadPathsArgs
	for _, path := range flag.Args() {
		abs, err := filepath.Abs(path)
		if err != nil {
			log.Fatalf("%q: %s", path, err.Error())
			return subcommands.ExitFailure
		}

		args.Trees = append(args.Trees, daemon.PreloadTree{
			Path:    abs,
			Pattern: c.pattern,
		})
	}
	out, err := client.PreloadPaths(&args)
	if err != nil {
		log.Printf("Preloading paths: %s", err.Error())
		return subcommands.ExitFailure
	}
	log.Printf("Uploaded %d files to the Llama store", out.Preloaded)
	return subcommands.ExitSuccess
}
