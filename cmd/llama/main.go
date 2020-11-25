package main

import (
	"context"
	"flag"
	"log"
	"os"
	"runtime/trace"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/google/subcommands"
	"github.com/nelhage/llama/cmd/internal/cli"
	"github.com/nelhage/llama/store/s3store"
)

func main() {
	subcommands.Register(subcommands.HelpCommand(), "")

	subcommands.Register(&InvokeCommand{}, "")

	subcommands.Register(&StoreCommand{}, "internals")
	subcommands.Register(&GetCommand{}, "internals")

	ctx := context.Background()
	code := runLlama(ctx)
	os.Exit(code)
}

func runLlama(ctx context.Context) int {
	var state cli.GlobalState
	debugAWS := false
	var traceFile string
	flag.StringVar(&state.Region, "region", "", "S3 region for commands")
	flag.StringVar(&state.Bucket, "bucket", "", "S3 bucket for the llama object store")
	flag.BoolVar(&debugAWS, "debug-aws", false, "Log all AWS requests/responses")
	flag.StringVar(&traceFile, "trace", "", "Log trace to file")

	flag.Parse()

	if state.Bucket == "" {
		state.Bucket = os.Getenv("LLAMA_BUCKET")
	}
	if traceFile != "" {
		f, err := os.Create(traceFile)
		if err != nil {
			log.Fatalf("open trace: %s", err.Error())
		}
		defer f.Close()
		trace.Start(f)
		defer trace.Stop()
	}

	ctx, task := trace.NewTask(ctx, "llama")
	defer task.End()

	trace.WithRegion(ctx, "global-init", func() {
		cfg := aws.NewConfig()
		if state.Region != "" {
			cfg = cfg.WithRegion(state.Region)
		}
		if debugAWS {
			cfg = cfg.WithLogLevel(aws.LogDebugWithHTTPBody)
		}
		state.Session = session.Must(session.NewSession(cfg))
		state.Store = s3store.FromSession(state.Session, state.Bucket)

		ctx = cli.WithState(ctx, &state)
	})

	return int(subcommands.Execute(ctx))
}
