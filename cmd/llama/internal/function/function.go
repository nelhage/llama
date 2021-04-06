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
	"bytes"
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/aws/aws-sdk-go/service/ecr"
	"github.com/google/subcommands"
	"github.com/nelhage/llama/cmd/internal/cli"
)

type UpdateFunctionCommand struct {
	buildRuntime string
	build        string
	tag          string
	memory       int64
	timeout      time.Duration

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
	flags.StringVar(&c.buildRuntime, "build-runtime", "", "Build a copy of the llama runtime image from a checkout")
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

	if cfg.tag != "" {
		if err := c.pushTag(ctx, global, cfg.tag); err != nil {
			log.Printf("Pushing image tag: %s", err.Error())
			return subcommands.ExitFailure
		}
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
		if c.buildRuntime != "" {
			log.Printf("Building the llama runtime from %s...", c.buildRuntime)
			cmd := exec.Command("docker", "build", "-t", "ghcr.io/nelhage/llama", c.buildRuntime)
			cmd.Stderr = os.Stderr
			cmd.Stdout = os.Stdout
			if err := runCmd(cmd); err != nil {
				return "", err
			}
		}
		log.Printf("Building image from %s...", c.build)
		cmd := exec.Command("docker", "build", "-t", tag, c.build)
		cmd.Stderr = os.Stderr
		cmd.Stdout = os.Stdout
		return tag, runCmd(cmd)
	} else {
		return "", nil
	}
}

func (c *UpdateFunctionCommand) pushTag(ctx context.Context, global *cli.GlobalState, tag string) error {
	err := runSh("docker", "push", tag)
	if err == nil {
		return nil
	}

	log.Printf("Authenticating to AWS ECR...")
	// Re-authenticate and try again
	ecrSvc := ecr.New(global.MustSession())
	resp, err := ecrSvc.GetAuthorizationToken(&ecr.GetAuthorizationTokenInput{})
	if err != nil {
		return err
	}
	auth := resp.AuthorizationData[0]
	decoded, err := base64.StdEncoding.DecodeString(*auth.AuthorizationToken)
	if err != nil {
		return err
	}
	colon := bytes.IndexByte(decoded, ':')
	cmd := exec.Command("docker", "login", "--username", string(decoded[:colon]), "--password-stdin",
		*auth.ProxyEndpoint)
	cmd.Stdin = bytes.NewBuffer(decoded[colon+1:])
	if err := runCmd(cmd); err != nil {
		return err
	}

	return runSh("docker", "push", tag)
}
