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
	"bufio"
	"context"
	"flag"
	"log"
	"os"
	"path"
	"strings"

	"github.com/google/subcommands"
	"github.com/nelhage/llama/cmd/internal/cli"
	"github.com/nelhage/llama/protocol"
)

type MultigetCommand struct {
}

func (*MultigetCommand) Name() string     { return "multiget" }
func (*MultigetCommand) Synopsis() string { return "Get multiple objects from the llama object store" }
func (*MultigetCommand) Usage() string {
	return `get HASHLIST
`
}

func (c *MultigetCommand) SetFlags(flags *flag.FlagSet) {
}

func (c *MultigetCommand) Execute(ctx context.Context, flag *flag.FlagSet, _ ...interface{}) subcommands.ExitStatus {
	state := cli.MustState(ctx)
	fh, err := os.Open(flag.Arg(0))
	if err != nil {
		log.Fatalf("open: %s", err.Error())
	}
	var files protocol.FileList
	scan := bufio.NewScanner(fh)
	for scan.Scan() {
		line := scan.Text()
		sp := strings.Index(line, "  ")
		hash := line[:sp]
		file := line[sp+2:]
		files = append(files, protocol.FileAndPath{
			Path: file,
			File: protocol.File{
				Mode: 0644,
				Blob: protocol.Blob{
					Ref: hash,
				},
			},
		})
		os.MkdirAll(path.Dir(file), 0755)
	}

	if err := files.Fetch(ctx, state.MustStore()); err != nil {
		log.Fatalf("fetch: %s", err.Error())
	}

	return subcommands.ExitSuccess
}
