package main

import (
	"context"
	"flag"
	"log"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/google/subcommands"
	"github.com/nelhage/llama/cmd/internal/cli"
	"github.com/nelhage/llama/daemon"
)

type DaemonCommand struct {
	ping             bool
	shutdown         bool
	start, autostart bool
	detach           bool
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
	flags.BoolVar(&c.start, "start", false, "Start the server")
	flags.BoolVar(&c.autostart, "autostart", false, "Start the server if it is not already running")
	flags.BoolVar(&c.detach, "detach", false, "Detach and run the server in the background")
}

func (c *DaemonCommand) Execute(ctx context.Context, flag *flag.FlagSet, _ ...interface{}) subcommands.ExitStatus {
	if c.ping || c.shutdown {
		client, err := daemon.Dial(ctx)
		defer client.Close()
		if err != nil {
			log.Fatalf("Connecting to daemon: %s", err.Error())
		}
		if c.ping {
			_, err = client.Ping(&daemon.PingArgs{})
			if err != nil {
				log.Fatalf("Pinging daemon: %s", err.Error())
			}
			log.Printf("The daemon is alive!")
		} else if c.shutdown {
			_, err = client.Shutdown(&daemon.ShutdownArgs{})
			if err != nil {
				log.Fatalf("Shutting down daemon: %s", err.Error())
			}
			log.Printf("The daemon is exiting.")
		}
		return subcommands.ExitSuccess
	} else if c.start || c.autostart {
		if c.autostart {
			client, err := daemon.Dial(ctx)
			if err == nil {
				_, err = client.Ping(&daemon.PingArgs{})
				client.Close()
			}
			if err == nil {
				log.Printf("The server is already running")
				return subcommands.ExitSuccess
			}
		}
		if c.detach {
			cmd := exec.Command("/proc/self/exe", "daemon", "-start")
			cmd.SysProcAttr = &syscall.SysProcAttr{
				Setpgid: true,

				// TODO: We can't clear our CTTY
				// unless we `setsid`, but if we
				// `setsid` we can't `setpgid`,
				// because then we would be moving
				// "into a process group in a new
				// session" (setpgid(2)). I believe
				// this is why UNIX daemons
				// traditionally double-fork, but I
				// don't know how to do that in Go.

				// Blocking SIGHUP alleviates the
				// worst problems with still being
				// part of the session, at least.

				// Setsid: true,
				// Noctty: true,
			}
			signal.Ignore(syscall.SIGHUP)
			if err := cmd.Start(); err != nil {
				log.Fatalf("Starting daemon: %s", err.Error())
			}
		} else {
			global := cli.MustState(ctx)
			if err := daemon.Start(ctx, global.Store, global.Session); err != nil {
				log.Fatalf("starting daemon: %s", err)
			}
		}
	} else {
		log.Fatalf("Must pass an action")
	}

	return subcommands.ExitSuccess
}
