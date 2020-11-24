package main

import (
	"context"
	"flag"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/google/subcommands"
	"github.com/nelhage/llama/cmd/internal/cli"
)

func main() {
	subcommands.Register(subcommands.HelpCommand(), "")

	subcommands.Register(&StoreCommand{}, "")
	subcommands.Register(&GetCommand{}, "")

	var state cli.GlobalState
	flag.StringVar(&state.Region, "region", "", "S3 region for commands")
	flag.StringVar(&state.Bucket, "bucket", "", "S3 bucket for the llama object store")

	flag.Parse()

	var cfg aws.Config
	if state.Region != "" {
		cfg.Region = &state.Region
	}
	state.Session = session.Must(session.NewSession(&cfg))

	ctx := context.Background()
	ctx = cli.WithState(ctx, &state)

	os.Exit(int(subcommands.Execute(ctx)))
}
