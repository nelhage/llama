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
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/google/subcommands"
	"github.com/nelhage/llama/cmd/internal/cli"
)

type ConfigCommand struct {
	shell bool
}

func (*ConfigCommand) Name() string     { return "config" }
func (*ConfigCommand) Synopsis() string { return "Read/Write llama configuration" }
func (*ConfigCommand) Usage() string {
	return `config [FLAGS]
`
}

func (c *ConfigCommand) SetFlags(flags *flag.FlagSet) {
	flags.BoolVar(&c.shell, "shell", false, "Write out AWS configuration as a set of shell assignments")
}

func (c *ConfigCommand) Execute(ctx context.Context, flag *flag.FlagSet, _ ...interface{}) subcommands.ExitStatus {
	if c.shell {
		return c.shellConfig(ctx)
	}
	log.Printf("config: Must specify an action")

	return subcommands.ExitFailure
}

func shellquote(word string) string {
	word = strings.ReplaceAll(word, `'`, `'"'"'`)
	return fmt.Sprintf(`'%s'`, word)
}

func (c *ConfigCommand) shellConfig(ctx context.Context) subcommands.ExitStatus {
	global := cli.MustState(ctx)

	if global.Config.Store == "" {
		log.Printf("llama config: Llama is not fully configured. Try running `llama bootstrap`?")
		return subcommands.ExitFailure
	}

	fmt.Fprintf(os.Stdout, "llama_object_store=%s\n", shellquote(global.Config.Store))
	fmt.Fprintf(os.Stdout, "llama_region=%s\n", shellquote(global.Config.Region))
	fmt.Fprintf(os.Stdout, "llama_iam_role=%s\n", shellquote(global.Config.IAMRole))
	fmt.Fprintf(os.Stdout, "llama_ecr_repository=%s\n", shellquote(global.Config.ECRRepository))
	return subcommands.ExitSuccess
}
