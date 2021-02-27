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
	"io/ioutil"
	"log"
	"os"
	"path"
	"strings"

	"github.com/google/subcommands"
	"github.com/nelhage/llama/cmd/internal/cli"
	"github.com/nelhage/llama/store"
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
	var paths []string
	var gets []store.GetRequest
	scan := bufio.NewScanner(fh)
	for scan.Scan() {
		line := scan.Text()
		sp := strings.Index(line, "  ")
		hash := line[:sp]
		file := line[sp+2:]
		gets = append(gets, store.GetRequest{Id: hash})
		paths = append(paths, file)
	}

	if err := scan.Err(); err != nil {
		log.Fatalf("scan: %s", err.Error())
	}

	state.MustStore().GetObjects(ctx, gets)

	for i, file := range paths {
		os.MkdirAll(path.Dir(file), 0755)
		if gets[i].Err != nil {
			log.Fatalf("get %s: %s", gets[i].Id, gets[i].Err.Error())
		}
		if err := ioutil.WriteFile(file, gets[i].Data, 0644); err != nil {
			log.Fatalf("write %s: %s", file, err.Error())
		}
	}

	return subcommands.ExitSuccess
}
