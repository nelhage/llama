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
	"bytes"
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"runtime/trace"
	"text/template"

	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/google/subcommands"
	"github.com/nelhage/llama/cmd/internal/cli"
	"github.com/nelhage/llama/cmd/internal/files"
	"github.com/nelhage/llama/llama"
	"github.com/nelhage/llama/protocol"
)

type InvokeCommand struct {
	stdin  bool
	logs   bool
	files  files.List
	output files.List
}

func (*InvokeCommand) Name() string     { return "invoke" }
func (*InvokeCommand) Synopsis() string { return "Invoke a llama command" }
func (*InvokeCommand) Usage() string {
	return `invoke FUNCTION-NAME ARGS...
`
}

func (c *InvokeCommand) SetFlags(flags *flag.FlagSet) {
	flags.BoolVar(&c.stdin, "stdin", false, "Read from stdin and pass it to the command")
	flags.BoolVar(&c.logs, "logs", false, "Display command invocation logs")
	flags.Var(&c.files, "f", "Pass a file through to the invocation")
	flags.Var(&c.files, "file", "Pass a file through to the invocation")
	flags.Var(&c.output, "o", "Fetch additional output files")
	flags.Var(&c.output, "output", "Fetch additional output files")
}

func (c *InvokeCommand) Execute(ctx context.Context, flag *flag.FlagSet, _ ...interface{}) subcommands.ExitStatus {
	global := cli.MustState(ctx)

	var spec protocol.InvocationSpec

	if c.stdin {
		stdin, err := ioutil.ReadAll(os.Stdin)
		if err != nil {
			log.Printf("reading stdin: %s", err.Error())
			return subcommands.ExitFailure
		}
		spec.Stdin, err = protocol.NewBlob(ctx, global.Store, stdin)
		if err != nil {
			log.Printf("writing to store: %s", err.Error())
			return subcommands.ExitFailure
		}
	}

	var err error
	spec.Files, err = c.files.Upload(ctx, global.Store, spec.Files)
	if err != nil {
		log.Println(err.Error())
		return subcommands.ExitFailure
	}

	var outputs files.List
	trace.WithRegion(ctx, "prepareArguments", func() {
		outputs, err = prepareArgs(ctx, global, &spec, flag.Args()[1:])
	})
	if err != nil {
		log.Println("preparing arguments: ", err.Error())
		return subcommands.ExitFailure
	}

	for _, out := range c.output {
		spec.Outputs = append(spec.Outputs, out.Remote)
	}
	outputs = outputs.Append(c.output...)

	svc := lambda.New(global.Session)
	response, err := llama.Invoke(ctx, svc, &llama.InvokeArgs{
		Function:   flag.Arg(0),
		Spec:       spec,
		ReturnLogs: c.logs,
	})
	if err != nil {
		if ir, ok := err.(*llama.ErrorReturn); ok {
			if ir.Logs != nil {
				fmt.Fprintf(os.Stderr, "==== invocation logs ====\n%s\n==== end logs ====\n", ir.Logs)
			}
		}
		log.Fatalf("invoke: %s", err.Error())
	}

	if response.Logs != nil {
		fmt.Fprintf(os.Stderr, "==== invocation logs ====\n%s\n==== end logs ====\n", response.Logs)
	}

	outputs.Fetch(ctx, global.Store, response.Response.Outputs)

	if response.Response.Stderr != nil {
		bytes, err := response.Response.Stderr.Read(ctx, global.Store)
		if err != nil {
			log.Printf("Reading stderr: %s", err.Error())
		} else {
			os.Stderr.Write(bytes)
		}
	}
	if response.Response.Stdout != nil {
		bytes, err := response.Response.Stdout.Read(ctx, global.Store)
		if err != nil {
			log.Printf("Reading stdout: %s", err.Error())
		} else {
			os.Stdout.Write(bytes)
		}
	}

	return subcommands.ExitStatus(response.Response.ExitStatus)
}

func prepareArgs(ctx context.Context, global *cli.GlobalState,
	spec *protocol.InvocationSpec,
	args []string) (files.List, error) {

	var ioctx files.IOContext
	rootTpl := template.New("<llama>")

	for i, arg := range args {
		tpl, err := rootTpl.New(fmt.Sprintf("arg-%d", i)).Parse(arg)
		if err != nil {
			return nil, err
		}
		var buf bytes.Buffer
		err = tpl.Execute(&buf, &ioctx)
		if err != nil {
			return nil, err
		}
		spec.Args = append(spec.Args, buf.String())
	}

	var err error
	if spec.Files, err = ioctx.Inputs.Upload(ctx, global.Store, spec.Files); err != nil {
		return nil, err
	}

	for _, f := range ioctx.Outputs {
		spec.Outputs = append(spec.Outputs, f.Remote)
	}

	return ioctx.Outputs, nil
}
