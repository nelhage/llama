package main

import (
	"context"
	"flag"
	"io/ioutil"
	"log"
	"os"

	"github.com/google/subcommands"
	"github.com/nelhage/llama/cmd/internal/cli"
	"github.com/nelhage/llama/store/s3store"
)

type StoreCommand struct {
}

func (*StoreCommand) Name() string     { return "store" }
func (*StoreCommand) Synopsis() string { return "Store an object to the llama object store" }
func (*StoreCommand) Usage() string {
	return `store PATH
`
}

func (c *StoreCommand) SetFlags(flags *flag.FlagSet) {
}

func (c *StoreCommand) Execute(ctx context.Context, flag *flag.FlagSet, _ ...interface{}) subcommands.ExitStatus {
	global := cli.MustState(ctx)

	store := s3store.FromSession(global.Session, global.Bucket)
	bytes, err := ioutil.ReadFile(flag.Arg(0))
	if err != nil {
		log.Printf("read %q: %v\n", flag.Arg(0), err)
		return subcommands.ExitFailure
	}

	id, err := store.Store(ctx, bytes)
	if err != nil {
		log.Printf("storing %q: %v\n", flag.Arg(0), err)
		return subcommands.ExitFailure
	}
	log.Printf("object stored id=%s", id)

	return subcommands.ExitSuccess
}

type GetCommand struct {
}

func (*GetCommand) Name() string     { return "get" }
func (*GetCommand) Synopsis() string { return "Get an object from the llama object store" }
func (*GetCommand) Usage() string {
	return `get ID
`
}

func (c *GetCommand) SetFlags(flags *flag.FlagSet) {
}

func (c *GetCommand) Execute(ctx context.Context, flag *flag.FlagSet, _ ...interface{}) subcommands.ExitStatus {
	global := cli.MustState(ctx)

	store := s3store.FromSession(global.Session, global.Bucket)
	obj, err := store.Get(ctx, flag.Arg(0))
	if err != nil {
		log.Printf("read %q: %v\n", flag.Arg(0), err)
		return subcommands.ExitFailure
	}
	os.Stdout.Write(obj)

	return subcommands.ExitSuccess
}
