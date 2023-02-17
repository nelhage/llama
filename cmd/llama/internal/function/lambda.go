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
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/nelhage/llama/cmd/internal/cli"
)

const (
	// Per https://docs.aws.amazon.com/lambda/latest/dg/configuration-memory.html,
	// this is the minimum amount of memory to get access to one
	// full core
	defaultMemory = 1769

	defaultTimeout = 60 * time.Second
)

func createOrUpdateFunction(ctx context.Context, g *cli.GlobalState, cfg *functionConfig) error {
	client := lambda.New(g.MustSession())
	args := &lambda.CreateFunctionInput{
		FunctionName: aws.String(cfg.name),
		Role:         aws.String(g.Config.IAMRole),
		Environment: &lambda.Environment{
			Variables: map[string]*string{
				"LLAMA_OBJECT_STORE": aws.String(g.Config.Store),
			},
		},
		Tags: map[string]*string{
			"LlamaFunction": aws.String("true"),
		},
		Code: &lambda.FunctionCode{
			ImageUri: aws.String(cfg.tag),
		},
		PackageType: aws.String(lambda.PackageTypeImage),
	}
	if cfg.memory != 0 {
		args.MemorySize = &cfg.memory
	} else {
		args.MemorySize = aws.Int64(defaultMemory)
	}
	if cfg.timeout != 0 {
		args.Timeout = aws.Int64(int64(cfg.timeout.Seconds()))
	} else {
		args.Timeout = aws.Int64(int64(defaultTimeout.Seconds()))
	}

	_, err := client.CreateFunction(args)
	if err == nil {
		return waitForFunction(ctx, client, cfg, "Creating")
	}
	if reqerr, ok := err.(awserr.RequestFailure); ok && reqerr.StatusCode() == 409 {
		return updateFunction(ctx, g, cfg)
	}
	return err
}

func updateFunction(ctx context.Context, g *cli.GlobalState, cfg *functionConfig) error {
	client := lambda.New(g.MustSession())
	args := &lambda.UpdateFunctionConfigurationInput{
		FunctionName: aws.String(cfg.name),
		Role:         aws.String(g.Config.IAMRole),
		Environment: &lambda.Environment{
			Variables: map[string]*string{
				"LLAMA_OBJECT_STORE": aws.String(g.Config.Store),
			},
		},
	}
	if cfg.memory != 0 {
		args.MemorySize = &cfg.memory
	}
	if cfg.timeout != 0 {
		args.Timeout = aws.Int64(int64(cfg.timeout.Seconds()))
	}

	if _, err := client.UpdateFunctionConfiguration(args); err != nil {
		return err
	}
	if err := waitForFunction(ctx, client, cfg, "Updating"); err != nil {
		return err
	}

	if cfg.tag != "" {
		codeArgs := &lambda.UpdateFunctionCodeInput{
			FunctionName: aws.String(cfg.name),
			ImageUri:     aws.String(cfg.tag),
		}
		if _, err := client.UpdateFunctionCode(codeArgs); err != nil {
			return err
		}
		return waitForFunction(ctx, client, cfg, "Deploying")
	}

	return nil
}

func waitForFunction(ctx context.Context, client *lambda.Lambda, config *functionConfig, prompt string) error {
	args := &lambda.GetFunctionInput{FunctionName: &config.name}
	log.Printf("%s function %s...", prompt, config.name)
	for {
		time.Sleep(3 * time.Second)
		out, err := client.GetFunction(args)
		if err != nil {
			log.Printf("Waiting for client: %s...", err.Error())
			continue
		}
		if out.Configuration.State != nil && *out.Configuration.State == lambda.StateFailed {
			return fmt.Errorf("Function update failed: %s", *out.Configuration.StateReason)
		}
		if out.Configuration.LastUpdateStatus == nil {
			continue
		}
		switch *out.Configuration.LastUpdateStatus {
		case lambda.LastUpdateStatusSuccessful:
			return nil
		case lambda.LastUpdateStatusInProgress:
			continue
		default:
			return fmt.Errorf("Unexpected function status: %s", *out.Configuration.LastUpdateStatus)
		}
	}
}
