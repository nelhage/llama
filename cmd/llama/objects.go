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
	"io/ioutil"
	"log"
	"os"

	"github.com/google/subcommands"
	"github.com/nelhage/llama/cmd/internal/cli"
	"github.com/nelhage/llama/store"
)

type StoreCommand struct {
}

func (*StoreCommand) Name() string     { return "store" }
func (*StoreCommand) Synopsis() string { return "Store an object to the llama object store" }
func (*StoreCommand) Usage() string {
	return `store PATH
`
}

func (c *StoreCommand) SetFlags(flags *flag.FlagSet) {
}

func (c *StoreCommand) Execute(ctx context.Context, flag *flag.FlagSet, _ ...interface{}) subcommands.ExitStatus {
	global := cli.MustState(ctx)

	for _, arg := range flag.Args() {
		bytes, err := ioutil.ReadFile(arg)
		if err != nil {
			log.Printf("read %q: %v\n", arg, err)
			return subcommands.ExitFailure
		}

		id, err := global.MustStore().Store(ctx, bytes)
		if err != nil {
			log.Printf("storing %q: %v\n", arg, err)
			return subcommands.ExitFailure
		}
		log.Printf("object %q stored id=%s", arg, id)
	}

	return subcommands.ExitSuccess
}

type GetCommand struct {
}

func (*GetCommand) Name() string     { return "get" }
func (*GetCommand) Synopsis() string { return "Get an object from the llama object store" }
func (*GetCommand) Usage() string {
	return `get ID
`
}

func (c *GetCommand) SetFlags(flags *flag.FlagSet) {
}

func (c *GetCommand) Execute(ctx context.Context, flag *flag.FlagSet, _ ...interface{}) subcommands.ExitStatus {
	global := cli.MustState(ctx)

	obj, err := store.Get(ctx, global.MustStore(), flag.Arg(0))
	if err != nil {
		log.Printf("read %q: %v\n", flag.Arg(0), err)
		return subcommands.ExitFailure
	}
	os.Stdout.Write(obj)

	return subcommands.ExitSuccess
}
