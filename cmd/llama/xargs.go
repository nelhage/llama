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
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"text/template"

	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/google/subcommands"
	"github.com/nelhage/llama/cmd/internal/cli"
	"github.com/nelhage/llama/files"
	"github.com/nelhage/llama/llama"
	"github.com/nelhage/llama/protocol"
	protocol_files "github.com/nelhage/llama/protocol/files"
	"github.com/nelhage/llama/store"
)

type XargsCommand struct {
	logs        bool
	files       files.List
	concurrency int

	lambda   *lambda.Lambda
	function string
	fileMap  protocol.FileList
}

func (*XargsCommand) Name() string     { return "xargs" }
func (*XargsCommand) Synopsis() string { return "Invoke a llama command over a list of inputs" }
func (*XargsCommand) Usage() string {
	return `invoke FUNCTION-NAME ARGS... < INPUTS
`
}

func (c *XargsCommand) SetFlags(flags *flag.FlagSet) {
	flags.BoolVar(&c.logs, "logs", false, "Display command invocation logs")
	flags.Var(&c.files, "f", "Pass a file through to the invocation")
	flags.Var(&c.files, "file", "Pass a file through to the invocation")
	flags.IntVar(&c.concurrency, "j", 100, "Number of concurrent lambdas to execute")
}

type Invocation struct {
	FormattedArgs   []string
	TemplateContext jobContext
	Templates       []*template.Template
	Args            *llama.InvokeArgs
	OutputPaths     map[string]string
	Result          *llama.InvokeResult
	Err             error
}

func (c *XargsCommand) Execute(ctx context.Context, flag *flag.FlagSet, _ ...interface{}) subcommands.ExitStatus {
	global := cli.MustState(ctx)

	var err error
	if len(c.files) > 0 {
		c.fileMap, err = c.files.Upload(ctx, global.MustStore(), c.fileMap)
		if err != nil {
			log.Fatalf("files: %s", err.Error())
		}
	}
	c.lambda = lambda.New(global.MustSession())
	c.function = flag.Arg(0)

	submit := make(chan *Invocation)
	go generateJobs(ctx, os.Stdin, flag.Args()[1:], submit)
	results := make(chan *Invocation)

	var wg sync.WaitGroup
	wg.Add(c.concurrency)
	go func() {
		wg.Wait()
		close(results)
	}()
	for i := 0; i < c.concurrency; i++ {
		go func() {
			defer wg.Done()
			c.worker(ctx, submit, results)
		}()
	}

	code := subcommands.ExitSuccess
	for done := range results {
		if done.Err != nil || done.Result.Response.ExitStatus != 0 {
			code = subcommands.ExitFailure
		}
		displayCmd := append([]string{c.function}, done.FormattedArgs...)
		if done.Err == nil && done.Result.Response.ExitStatus == 0 {
			log.Printf("Done: %v", displayCmd)
			continue
		}

		if done.Err == nil {
			log.Printf("Command exited with status: %v: %d", displayCmd, done.Result.Response.ExitStatus)
		} else if done.Err != nil {
			log.Printf("Invocation failed: %v: %s", displayCmd, done.Err.Error())
			if ret, ok := done.Err.(*llama.ErrorReturn); ok {
				if ret.Logs != nil {
					log.Printf("==== logs ====\n%s\n==== end logs ====\n", ret.Logs)
				}
			}
		}
		if done.Result == nil {
			continue
		}
		if done.Result.Logs != nil {
			log.Printf("==== logs ====\n%s\n==== end logs ====\n", done.Result.Logs)
		}
		if done.Result.Response.Stdout != nil {
			stdout, err := protocol_files.Read(ctx, global.MustStore(), done.Result.Response.Stdout)
			if err == nil {
				log.Printf("==== stdout ====\n%s\n==== end stdout ====\n", stdout)
			}
		}
		if done.Result.Response.Stderr != nil {
			stderr, err := protocol_files.Read(ctx, global.MustStore(), done.Result.Response.Stderr)
			if err == nil {
				log.Printf("==== stderr ====\n%s\n==== end stderr ====\n", stderr)
			}
		}
	}

	return code
}

func prepareTemplates(args []string) ([]*template.Template, error) {
	var argTemplates []*template.Template
	for i, arg := range args {
		tpl, err := template.New(fmt.Sprintf("arg-%d", i)).Parse(arg)
		if err != nil {
			return nil, fmt.Errorf("template parse error: %q: %w", arg, err)
		}
		argTemplates = append(argTemplates, tpl)
	}
	return argTemplates, nil
}

func generateJobs(ctx context.Context, lines io.Reader, args []string, out chan<- *Invocation) {
	argTemplates, err := prepareTemplates(args)
	if err != nil {
		log.Fatal(err)
	}

	defer close(out)
	read := bufio.NewReader(lines)

	i := -1
	for {
		i += 1
		line, err := read.ReadString('\n')
		if err == io.EOF {
			return
		}
		if err != nil {
			log.Fatalf("read stdin: %s", err.Error())
		}
		line = strings.TrimRight(line, "\n")
		job := Invocation{
			TemplateContext: jobContext{
				Idx:  i,
				Line: line,
			},
			Templates: argTemplates,
		}
		out <- &job
	}
}

func (c *XargsCommand) worker(ctx context.Context, jobs <-chan *Invocation, out chan<- *Invocation) {
	global := cli.MustState(ctx)
	for job := range jobs {
		c.run(ctx, global, job)
		out <- job
	}
}

// The context object passed to template.Template.Execute for each
// input line
type jobContext struct {
	files.IOContext
	Idx  int
	Line string
}

func (j *jobContext) AsFile(data string) string {
	dest := fmt.Sprintf("llama/tmp.%d", len(j.Inputs))
	if !strings.HasSuffix(data, "\n") {
		data = data + "\n"
	}
	j.Inputs = j.Inputs.Append(files.Mapped{
		Local: files.LocalFile{
			Bytes: []byte(data),
		},
		Remote: dest,
	})
	return dest
}

func prepareInvocation(ctx context.Context,
	store store.Store,
	globalFiles protocol.FileList,
	job *Invocation) (*protocol.InvocationSpec, error) {
	for _, tpl := range job.Templates {
		var w bytes.Buffer
		err := tpl.Execute(&w, &job.TemplateContext)
		if err != nil {
			return nil, err
		}
		job.FormattedArgs = append(job.FormattedArgs, w.String())
	}

	var allFiles protocol.FileList
	allFiles, err := job.TemplateContext.Inputs.Upload(ctx, store, globalFiles)
	if err != nil {
		return nil, err
	}

	var outputs []string
	for _, f := range job.TemplateContext.Outputs {
		outputs = append(outputs, f.Remote)
	}

	return &protocol.InvocationSpec{
		Args:    job.FormattedArgs,
		Files:   allFiles,
		Outputs: outputs,
	}, nil
}

func (c *XargsCommand) run(ctx context.Context, global *cli.GlobalState, job *Invocation) {
	st := global.MustStore()
	spec, err := prepareInvocation(ctx, st, c.fileMap, job)
	if err != nil {
		job.Err = err
		return
	}
	job.Args = &llama.InvokeArgs{
		Function:   c.function,
		ReturnLogs: c.logs,
		Spec:       *spec,
	}

	if job.Err != nil {
		return
	}
	job.Result, job.Err = llama.Invoke(ctx, c.lambda, st, job.Args)

	if job.Err == nil {
		fetchList, extra := job.TemplateContext.Outputs.TransformToLocal(ctx, job.Result.Response.Outputs)
		for _, out := range extra {
			log.Printf("Remote returned unexpected output: %s", out.Path)
		}
		var gets []store.GetRequest
		for _, file := range fetchList {
			gets = protocol_files.AppendGet(gets, &file.Blob)
		}
		st.GetObjects(ctx, gets)
		for _, file := range fetchList {
			err, gets = protocol_files.FetchFile(&file.File, file.Path, gets)
			if err != nil {
				job.Err = err
				break
			}
		}
	}
}
