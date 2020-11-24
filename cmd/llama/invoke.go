package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/google/subcommands"
	"github.com/nelhage/llama/cmd/internal/cli"
	"github.com/nelhage/llama/protocol"
)

type InvokeCommand struct {
	stdin bool
	logs  bool
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

	var outputs map[string]string
	var err error
	spec.Args, outputs, err = prepareArgs(ctx, global, flag.Args()[1:])
	if err != nil {
		log.Printf("preparing arguments: ", err.Error())
		return subcommands.ExitFailure
	}

	payload, err := json.Marshal(&spec)
	if err != nil {
		log.Fatalf("marshal: %s", err.Error())
	}

	svc := lambda.New(global.Session)
	input := lambda.InvokeInput{
		FunctionName: aws.String(flag.Arg(0)),
		Payload:      payload,
	}
	if c.logs {
		input.LogType = aws.String(lambda.LogTypeTail)
	}

	resp, err := svc.Invoke(&input)
	if err != nil {
		log.Printf("Invoking: %s", err.Error())
		return subcommands.ExitFailure
	}

	if resp.LogResult != nil {
		fmt.Fprintf(os.Stderr, "==== invocation logs ====\n%s", *resp.LogResult)
	}

	var reply protocol.InvocationResponse
	if err := json.Unmarshal(resp.Payload, &reply); err != nil {
		log.Printf("unmarshal payload: %s", err.Error())
	}

	for key, blob := range reply.Outputs {
		file, ok := outputs[key]
		if !ok {
			log.Printf("Unexpected output: %q", key)
			continue
		}
		data, err := blob.Read(ctx, global.Store)
		if err != nil {
			log.Printf("reading output %q: %s", key, err.Error())
			continue
		}
		if err := ioutil.WriteFile(file, data, 0644); err != nil {
			log.Printf("reading output %q: %s", file, err.Error())
		}
	}

	if reply.Stderr != nil {
		bytes, err := reply.Stderr.Read(ctx, global.Store)
		if err != nil {
			log.Printf("Reading stderr: %s", err.Error())
		} else {
			os.Stderr.Write(bytes)
		}
	}
	if reply.Stdout != nil {
		bytes, err := reply.Stdout.Read(ctx, global.Store)
		if err != nil {
			log.Printf("Reading stdout: %s", err.Error())
		} else {
			os.Stdout.Write(bytes)
		}
	}

	return subcommands.ExitStatus(reply.ExitStatus)
}

func prepareArgs(ctx context.Context, global *cli.GlobalState, args []string) ([]json.RawMessage, map[string]string, error) {
	out := make([]json.RawMessage, len(args))
	var outputs map[string]string
	for i, arg := range args {
		var argSpec interface{} = arg
		idx := strings.Index(arg, "@")
		if idx > 0 {
			pfx := arg[:idx]
			arg = arg[idx+1:]

			var a protocol.Arg

			if pfx == "i" || pfx == "io" {
				data, err := ioutil.ReadFile(arg)
				if err != nil {
					return nil, nil, fmt.Errorf("Reading file: %q: %w", arg, err)

				}
				a.In, err = protocol.NewBlob(ctx, global.Store, data)
				if err != nil {
					return nil, nil, fmt.Errorf("Writing to store: %q: %w", arg, err)
				}
				argSpec = a
			}
			if pfx == "o" || pfx == "io" {
				name := fmt.Sprintf("%s-%d", path.Base(arg), i)
				a.Out = &name
				argSpec = a
				if outputs == nil {
					outputs = make(map[string]string)
				}
				outputs[name] = arg
			}
		}
		word, err := json.Marshal(argSpec)
		if err != nil {
			log.Fatal("marshal: ", err)
		}
		out[i] = json.RawMessage(word)
	}
	return out, outputs, nil
}
