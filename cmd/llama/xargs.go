package main

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"runtime/trace"
	"strings"
	"sync"
	"text/template"

	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/google/subcommands"
	"github.com/nelhage/llama/cmd/internal/cli"
	"github.com/nelhage/llama/llama"
	"github.com/nelhage/llama/protocol"
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
	StrArgs         []string
	TemplateContext jobContext
	Args            *llama.InvokeArgs
	OutputPaths     map[string]string
	Result          *llama.InvokeResult
	Err             error
}

func (c *XargsCommand) Execute(ctx context.Context, flag *flag.FlagSet, _ ...interface{}) subcommands.ExitStatus {
	global := cli.MustState(ctx)
	var err error
	c.fileMap, err = c.files.Prepare(ctx, global.Store)
	if err != nil {
		log.Fatalf("files: %s", err.Error())
	}
	c.lambda = lambda.New(global.Session)

	c.function = flag.Arg(0)
	var argTemplates []*template.Template
	for i, arg := range flag.Args()[1:] {
		tpl, err := template.New(fmt.Sprintf("arg-%d", i)).Parse(arg)
		if err != nil {
			log.Fatalf("template parse error: %q: %s", arg, err.Error())
		}
		argTemplates = append(argTemplates, tpl)
	}

	submit := make(chan *Invocation)
	go generateJobs(ctx, argTemplates, submit)
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
		displayCmd := append([]string{c.function}, done.StrArgs...)
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

// The context object paassed to template.Template.Execute for each
type jobContext struct {
	I        int
	Line     string
	tempPath string
}

func (j *jobContext) File() (string, error) {
	if j.tempPath == "" {
		fh, err := ioutil.TempFile("", "llama.*")
		if err != nil {
			return "", err
		}

		_, err = fh.WriteString(j.Line)
		if err == nil {
			_, err = fh.Write([]byte{'\n'})
		}
		j.tempPath = fh.Name()
		if err == nil {
			err = fh.Close()
		}
		if err != nil {
			return "", err
		}
	}
	return j.tempPath, nil
}

func (j *jobContext) Cleanup() {
	if j.tempPath != "" {
		os.Remove(j.tempPath)
	}
}

func generateJobs(ctx context.Context, templates []*template.Template, out chan<- *Invocation) {
	defer close(out)
	read := bufio.NewReader(os.Stdin)

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
				I:    i,
				Line: line,
			},
		}
		for _, tpl := range templates {
			var w bytes.Buffer
			err := tpl.Execute(&w, &job.TemplateContext)
			if err != nil {
				job.Err = err
				break
			}
			job.StrArgs = append(job.StrArgs, w.String())
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

func (c *XargsCommand) run(ctx context.Context, global *cli.GlobalState, job *Invocation) {
	job.Args = &llama.InvokeArgs{
		Function:   c.function,
		ReturnLogs: c.logs,
		Spec: protocol.InvocationSpec{
			Files: c.fileMap,
		},
	}
	trace.WithRegion(ctx, "prepareArgs", func() {
		for _, arg := range job.StrArgs {
			word, err := parseArg(ctx, &job.OutputPaths, arg)
			if err != nil {
				job.Err = err
				return
			}
			job.Args.Spec.Args = append(job.Args.Spec.Args, word)
		}
	})
	job.TemplateContext.Cleanup()
	if job.Err != nil {
		return
	}

	job.Result, job.Err = llama.Invoke(ctx, c.lambda, job.Args)

	if job.Err == nil {
		fetchOutputs(ctx, job.OutputPaths, &job.Result.Response)
	}
}
