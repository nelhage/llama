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

package function

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/google/subcommands"
	"github.com/nelhage/llama/cmd/internal/cli"
)

type UpdateFunctionCommand struct {
	build   string
	tag     string
	memory  int64
	timeout time.Duration

	create bool
}

type functionConfig struct {
	name string

	tag     string
	memory  int64
	timeout time.Duration
}

func (*UpdateFunctionCommand) Name() string     { return "update-function" }
func (*UpdateFunctionCommand) Synopsis() string { return "Create or update a llama Lambda function" }
func (*UpdateFunctionCommand) Usage() string {
	return `update-function [options] FUNCTION-NAME
`
}

func (c *UpdateFunctionCommand) SetFlags(flags *flag.FlagSet) {
	flags.StringVar(&c.build, "build", "", "Build a docker image out of the path for the function image")
	flags.StringVar(&c.tag, "tag", "", "Use the specified tag for the function image")

	flags.Int64Var(&c.memory, "memory", 0, "Specify the function memory size, in MB")
	flags.DurationVar(&c.timeout, "timeout", 0, "Specify the function timeout")

	flags.BoolVar(&c.create, "create", false, "Create the function if it does not exist")
}

func (c *UpdateFunctionCommand) Execute(ctx context.Context, flag *flag.FlagSet, _ ...interface{}) subcommands.ExitStatus {
	global := cli.MustState(ctx)
	args := flag.Args()
	if len(args) != 1 {
		log.Printf("Usage: %s", c.Usage())
		return subcommands.ExitUsageError
	}

	var cfg functionConfig
	cfg.name = args[0]

	var err error
	cfg.tag, err = c.buildImage(ctx, global, cfg.name)
	if err != nil {
		log.Printf("Building image: %s", err.Error())
		return subcommands.ExitFailure
	}

	cfg.memory = c.memory
	cfg.timeout = c.timeout

	if c.create {
		err = createOrUpdateFunction(ctx, global, &cfg)
	} else {
		err = updateFunction(ctx, global, &cfg)
	}

	if err != nil {
		log.Printf("%s: %s", cfg.name, err.Error())
		return subcommands.ExitFailure
	}

	return subcommands.ExitSuccess
}

func (c *UpdateFunctionCommand) buildImage(ctx context.Context, global *cli.GlobalState, functionName string) (string, error) {
	tag := fmt.Sprintf("%s:%s", global.Config.ECRRepository, functionName)
	if c.build != "" && c.tag != "" {
		return "", fmt.Errorf("-build and -tag are mutually exclusive")
	} else if c.tag != "" {
		if err := runSh("docker", "tag", c.tag, tag); err != nil {
			return "", err
		}
		return tag, nil
	} else if c.build != "" {
		log.Printf("Building image from %s...", c.build)
		cmd := exec.Command("docker", "build", "-t", tag, c.build)
		cmd.Stderr = os.Stderr
		cmd.Stdout = os.Stdout
		return tag, cmd.Run()
	} else {
		return "", nil
	}
}
