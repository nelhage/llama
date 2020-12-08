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
	"github.com/nelhage/llama/llama"
	"github.com/nelhage/llama/protocol"
	"github.com/nelhage/llama/store"
)

type XargsCommand struct {
	logs        bool
	files       fileList
	concurrency int

	lambda   *lambda.Lambda
	function string
	fileMap  map[string]protocol.File
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
	if len(c.files.files) > 0 {
		c.fileMap = make(map[string]protocol.File, len(c.files.files))
		err = c.files.Upload(ctx, global.Store, c.fileMap)
		if err != nil {
			log.Fatalf("files: %s", err.Error())
		}
	}
	c.lambda = lambda.New(global.Session)
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
		}
		if done.Result == nil {
			continue
		}
		if done.Result.Logs != nil {
			log.Printf("==== logs ====\n%s\n==== end logs ====\n", done.Result.Logs)
		}
		if done.Result.Response.Stdout != nil {
			stdout, err := done.Result.Response.Stdout.Read(ctx, global.Store)
			if err == nil {
				log.Printf("==== stdout ====\n%s\n==== end stdout ====\n", stdout)
			}
		}
		if done.Result.Response.Stderr != nil {
			stderr, err := done.Result.Response.Stderr.Read(ctx, global.Store)
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
	ioContext
	ExtraFiles map[string][]byte
	Idx        int
	Line       string
}

func (j *jobContext) AsFile(data string) string {
	if j.ExtraFiles == nil {
		j.ExtraFiles = make(map[string][]byte)
	}
	dest := fmt.Sprintf("llama/tmp.%d", len(j.ExtraFiles))
	if !strings.HasSuffix(data, "\n") {
		data = data + "\n"
	}
	j.ExtraFiles[dest] = []byte(data)
	return dest
}

func prepareInvocation(ctx context.Context,
	store store.Store,
	globalFiles map[string]protocol.File,
	job *Invocation) (*protocol.InvocationSpec, error) {
	for _, tpl := range job.Templates {
		var w bytes.Buffer
		err := tpl.Execute(&w, &job.TemplateContext)
		if err != nil {
			return nil, err
		}
		job.FormattedArgs = append(job.FormattedArgs, w.String())
	}

	var files map[string]protocol.File
	if len(job.TemplateContext.files.files) > 0 || len(job.TemplateContext.ExtraFiles) > 0 {
		files = make(map[string]protocol.File)
		if err := job.TemplateContext.files.Upload(ctx, store, files); err != nil {
			return nil, err
		}
		for key, v := range job.TemplateContext.ExtraFiles {
			blob, err := protocol.NewBlob(ctx, store, v)
			if err != nil {
				return nil, err
			}
			files[key] = protocol.File{Mode: 0644, Blob: *blob}
		}
	}

	var outputs []string
	for _, f := range job.TemplateContext.outputs.files {
		outputs = append(outputs, f.remote)
	}

	if globalFiles != nil {
		if files == nil {
			files = globalFiles
		} else {
			for k, v := range globalFiles {
				files[k] = v
			}
		}
	}

	return &protocol.InvocationSpec{
		Args:    job.FormattedArgs,
		Files:   files,
		Outputs: outputs,
	}, nil
}

func (c *XargsCommand) run(ctx context.Context, global *cli.GlobalState, job *Invocation) {
	spec, err := prepareInvocation(ctx, global.Store, c.fileMap, job)
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
	job.Result, job.Err = llama.Invoke(ctx, c.lambda, job.Args)

	if job.Err == nil {
		fetchOutputs(ctx, job.TemplateContext.outputs.files, &job.Result.Response)
	}
}
