package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"runtime/trace"
	"strings"
	"text/template"

	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/google/subcommands"
	"github.com/nelhage/llama/cmd/internal/cli"
	"github.com/nelhage/llama/llama"
	"github.com/nelhage/llama/protocol"
	"github.com/nelhage/llama/store"
)

type mappedFile struct {
	local  string
	remote string
}

type fileList struct {
	files []mappedFile
}

func (f *fileList) String() string {
	return ""
}

func (f *fileList) Get() interface{} {
	return f.files
}

func (f *fileList) Set(v string) error {
	idx := strings.IndexRune(v, ':')
	var source, dest string
	if idx > 0 {
		source = v[:idx]
		dest = v[idx+1:]
	} else {
		source = v
		dest = v
	}
	if path.IsAbs(dest) {
		return fmt.Errorf("-file: cannot expose file at absolute path: %q", dest)
	}
	f.files = append(f.files, mappedFile{source, dest})
	return nil
}

func (f *fileList) Upload(ctx context.Context, store store.Store, files map[string]protocol.File) error {
	var outErr error
	trace.WithRegion(ctx, "uploadFiles", func() {
		for _, file := range f.files {
			data, err := ioutil.ReadFile(file.local)
			if err != nil {
				outErr = fmt.Errorf("reading file %q: %w", file.local, err)
				return
			}
			st, err := os.Stat(file.local)
			if err != nil {
				outErr = fmt.Errorf("stat %q: %w", file.local, err)
				return
			}
			blob, err := protocol.NewBlob(ctx, store, data)
			if err != nil {
				outErr = err
				return
			}
			files[file.remote] = protocol.File{Blob: *blob, Mode: st.Mode()}
		}
	})
	if outErr != nil {
		return outErr
	}
	return nil
}

type InvokeCommand struct {
	stdin bool
	logs  bool
	files fileList
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
	if len(c.files.files) > 0 {
		spec.Files = make(map[string]protocol.File, len(c.files.files))
		err = c.files.Upload(ctx, global.Store, spec.Files)
		if err != nil {
			log.Println(err.Error())
			return subcommands.ExitFailure
		}
	}

	var outputs []mappedFile
	trace.WithRegion(ctx, "prepareArguments", func() {
		outputs, err = prepareArgs(ctx, global, &spec, flag.Args()[1:])
	})
	if err != nil {
		log.Println("preparing arguments: ", err.Error())
		return subcommands.ExitFailure
	}

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

	fetchOutputs(ctx, outputs, &response.Response)

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

func fetchOutputs(ctx context.Context, outputs []mappedFile, resp *protocol.InvocationResponse) {
	trace.WithRegion(ctx, "fetchOutputs", func() {
		global := cli.MustState(ctx)
		for _, out := range outputs {
			blob, ok := resp.Outputs[out.remote]
			if !ok {
				log.Printf("Invocation is missing file: %q", out.local)
				continue
			}
			if err := blob.Fetch(ctx, global.Store, out.local); err != nil {
				log.Printf("Fetch %q: %s", out.local, err.Error())
			}

		}
	})
}

type ioContext struct {
	files   fileList
	outputs fileList
}

func (a *ioContext) cleanPath(file string) (mappedFile, error) {
	if path.IsAbs(file) {
		return mappedFile{}, fmt.Errorf("Cannot pass absolute path: %q", file)
	}
	file = path.Clean(file)
	if strings.HasPrefix(file, "../") {
		return mappedFile{}, fmt.Errorf("Cannot pass path outside working directory: %q", file)
	}
	return mappedFile{file, file}, nil
}

func (a *ioContext) Input(file string) (string, error) {
	mapped, err := a.cleanPath(file)
	if err != nil {
		return "", err
	}
	a.files.files = append(a.files.files, mapped)
	return mapped.remote, nil
}

func (a *ioContext) I(file string) (string, error) {
	return a.Input(file)
}

func (a *ioContext) Output(file string) (string, error) {
	mapped, err := a.cleanPath(file)
	if err != nil {
		return "", err
	}
	a.outputs.files = append(a.outputs.files, mapped)
	return mapped.remote, nil
}

func (a *ioContext) O(file string) (string, error) {
	return a.Output(file)
}

func (a *ioContext) InputOutput(file string) (string, error) {
	mapped, err := a.cleanPath(file)
	if err != nil {
		return "", err
	}
	a.files.files = append(a.files.files, mapped)
	a.outputs.files = append(a.outputs.files, mapped)
	return mapped.remote, nil
}

func (a *ioContext) IO(file string) (string, error) {
	return a.InputOutput(file)
}

func prepareArgs(ctx context.Context, global *cli.GlobalState,
	spec *protocol.InvocationSpec,
	args []string) ([]mappedFile, error) {

	var ioctx ioContext
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

	if len(ioctx.files.files) > 0 && spec.Files == nil {
		spec.Files = make(map[string]protocol.File, len(ioctx.files.files))
	}
	if err := ioctx.files.Upload(ctx, global.Store, spec.Files); err != nil {
		return nil, err
	}

	for _, f := range ioctx.outputs.files {
		spec.Outputs = append(spec.Outputs, f.remote)
	}

	return ioctx.outputs.files, nil
}
