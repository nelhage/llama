package main

import (
	"context"
	"flag"
	"log"

	"github.com/google/subcommands"
	"github.com/nelhage/llama/cmd/internal/cli"
	"github.com/nelhage/llama/daemon"
)

type DaemonCommand struct {
	ping     bool
	shutdown bool
}

func (*DaemonCommand) Name() string     { return "daemon" }
func (*DaemonCommand) Synopsis() string { return "Start or interact with the Llama daemon" }
func (*DaemonCommand) Usage() string {
	return `daemon [flags]
`
}

func (c *DaemonCommand) SetFlags(flags *flag.FlagSet) {
	flags.BoolVar(&c.ping, "ping", false, "Check if the server is running")
	flags.BoolVar(&c.shutdown, "shutdown", false, "Stop the running server")
}

func (c *DaemonCommand) Execute(ctx context.Context, flag *flag.FlagSet, _ ...interface{}) subcommands.ExitStatus {
	if c.ping {
		client, err := daemon.Dial(ctx)
		if err != nil {
			log.Fatalf("Connecting to daemon: %s", err.Error())
		}
		_, err = client.Ping(&daemon.PingArgs{})
		if err != nil {
			log.Fatalf("Pinging daemon: %s", err.Error())
		}
		log.Printf("The daemon is alive!")
		return subcommands.ExitSuccess
	}

	if c.shutdown {
		client, err := daemon.Dial(ctx)
		if err != nil {
			log.Fatalf("Connecting to daemon: %s", err.Error())
		}
		_, err = client.Shutdown(&daemon.ShutdownArgs{})
		if err != nil {
			log.Fatalf("Shutting down daemon: %s", err.Error())
		}
		log.Printf("The daemon is exiting.")
		return subcommands.ExitSuccess
	}

	global := cli.MustState(ctx)
	if err := daemon.Start(ctx, global.Store, global.Session); err != nil {
		log.Fatalf("starting daemon: %s", err)
	}

	return subcommands.ExitSuccess
}
