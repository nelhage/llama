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

	"github.com/google/subcommands"
	"github.com/nelhage/llama/cmd/internal/cli"
	"github.com/nelhage/llama/cmd/llama/internal/server"
	"github.com/nelhage/llama/daemon"
	"github.com/nelhage/llama/files"
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

	var args daemon.InvokeWithFilesArgs

	if c.stdin {
		stdin, err := ioutil.ReadAll(os.Stdin)
		if err != nil {
			log.Printf("reading stdin: %s", err.Error())
			return subcommands.ExitFailure
		}
		args.Stdin = stdin
	}

	var err error
	trace.WithRegion(ctx, "prepareArguments", func() {
		var ioctx files.IOContext
		args.Args, ioctx, err = prepareArgs(ctx, global, flag.Args()[1:])
		args.Files = c.files.Append(ioctx.Inputs...)
		args.Outputs = c.output.Append(ioctx.Outputs...)
	})
	if err != nil {
		log.Println("preparing arguments: ", err.Error())
		return subcommands.ExitFailure
	}

	cl, err := server.DialWithAutostart(ctx, cli.SocketPath())
	if err != nil {
		log.Fatalf("connecting to daemon: %s", err.Error())
	}
	args.Function = flag.Arg(0)
	args.ReturnLogs = c.logs

	wd, err := os.Getwd()
	if err != nil {
		log.Fatalf("getcwd: %s", err.Error())
	}
	args.Files = args.Files.MakeAbsolute(wd)
	args.Outputs = args.Outputs.MakeAbsolute(wd)

	response, err := cl.InvokeWithFiles(&args)
	if err != nil {
		log.Fatalf("invoke: %s", err.Error())
	}
	if response.Logs != nil {
		fmt.Fprintf(os.Stderr, "==== invocation logs ====\n%s\n==== end logs ====\n", response.Logs)
	}

	if response.Stdout != nil {
		os.Stdout.Write(response.Stdout)
	}
	if response.Stderr != nil {
		os.Stderr.Write(response.Stderr)
	}

	if response.InvokeErr != "" {
		log.Fatalf("invoke: %s", response.InvokeErr)
	}

	/*		if ir, ok := err.(*llama.ErrorReturn); ok {
				if ir.Logs != nil {
					fmt.Fprintf(os.Stderr, "==== invocation logs ====\n%s\n==== end logs ====\n", ir.Logs)
				}
			}
	*/

	return subcommands.ExitStatus(response.ExitStatus)
}

func prepareArgs(ctx context.Context, global *cli.GlobalState, args []string) ([]string, files.IOContext, error) {
	var ioctx files.IOContext
	rootTpl := template.New("<llama>")

	var outArgs []string
	for i, arg := range args {
		tpl, err := rootTpl.New(fmt.Sprintf("arg-%d", i)).Parse(arg)
		if err != nil {
			return nil, files.IOContext{}, err
		}
		var buf bytes.Buffer
		err = tpl.Execute(&buf, &ioctx)
		if err != nil {
			return nil, files.IOContext{}, err
		}
		outArgs = append(outArgs, buf.String())
	}

	return outArgs, ioctx, nil
}
