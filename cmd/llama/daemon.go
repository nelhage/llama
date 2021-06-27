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
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/google/subcommands"
	"github.com/nelhage/llama/cmd/internal/cli"
	"github.com/nelhage/llama/daemon"
	"github.com/nelhage/llama/daemon/server"
	"golang.org/x/sys/unix"
)

type DaemonCommand struct {
	path             string
	ping             bool
	shutdown         bool
	stats            bool
	start, autostart bool
	detach           bool
	idleTimeout      time.Duration
	ccConcurrency    int64
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
	flags.BoolVar(&c.stats, "stats", false, "Show server statistics")
	flags.BoolVar(&c.autostart, "autostart", false, "Start the server if it is not already running")
	flags.BoolVar(&c.detach, "detach", false, "Detach and run the server in the background")
	flags.StringVar(&c.path, "path", cli.SocketPath(), "Path to daemon socket")
	flags.DurationVar(&c.idleTimeout, "idle-timeout", 10*time.Minute, "Idle timeout")
	flags.Int64Var(&c.ccConcurrency, "cc-concurrency", 0, "Configure llamacc concurrency limit")
}

func raiseRlimits() {
	var limits unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &limits); err != nil {
		log.Printf("Warning: Unable to read RLIMIT_NOFILE: %s", err.Error())
		return
	}
	target := uint64(65535)
	limits.Cur = target
	if limits.Cur > limits.Max {
		limits.Cur = limits.Max
	}
	if err := unix.Setrlimit(unix.RLIMIT_NOFILE, &limits); err != nil {
		log.Printf("Warning: setting RLIMIT_NOFILE: %s", err.Error())
	}
}

func (c *DaemonCommand) Execute(ctx context.Context, flag *flag.FlagSet, _ ...interface{}) subcommands.ExitStatus {
	if c.ping || c.shutdown || c.stats {
		client, err := daemon.Dial(ctx, c.path)
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
		} else if c.stats {
			stats, err := client.GetDaemonStats(&daemon.StatsArgs{})
			if err != nil {
				log.Fatalf("Getting stats: %s", err.Error())
			}
			fmt.Fprintf(os.Stdout, "in_flight=%d\n", stats.Stats.InFlight)
			fmt.Fprintf(os.Stdout, "max_in_flight=%d\n", stats.Stats.MaxInFlight)
			fmt.Fprintf(os.Stdout, "invocations=%d\n", stats.Stats.Invocations)
			fmt.Fprintf(os.Stdout, "func_errors=%d\n", stats.Stats.FunctionErrors)
			fmt.Fprintf(os.Stdout, "other_errors=%d\n", stats.Stats.OtherErrors)
			fmt.Fprintf(os.Stdout, "AWS Usage:\n")
			cost := 0.0
			tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			fmt.Fprintf(tw, "  Lambda runtime\tms\t%d\n", stats.Stats.Usage.Lambda.Millis)
			fmt.Fprintf(tw, "  Lambda runtime\tMB-ms\t%d\t$%.2f\n",
				stats.Stats.Usage.Lambda.MB_Millis,
				float64(stats.Stats.Usage.Lambda.MB_Millis)*0.0000166667/1000000,
			)
			cost += float64(stats.Stats.Usage.Lambda.MB_Millis) * 0.0000166667 / 1000000
			fmt.Fprintf(tw, "  Lambda requests\t\t%d\t$%.2f\n",
				stats.Stats.Usage.Lambda.Requests,
				float64(stats.Stats.Usage.Lambda.Requests)*0.20/1000000,
			)
			cost += float64(stats.Stats.Usage.Lambda.Requests) * 0.20 / 1000000
			fmt.Fprintf(tw, "  S3 Write requests[client]\t\t%d\t$%.2f\n",
				stats.Stats.Usage.LocalS3.Write_Requests,
				0.005/1000*float64(stats.Stats.Usage.LocalS3.Write_Requests),
			)
			cost += 0.005 / 1000 * float64(stats.Stats.Usage.LocalS3.Write_Requests)
			fmt.Fprintf(tw, "  S3 Read requests[client]\t\t%d\t$%.2f\n",
				stats.Stats.Usage.LocalS3.Read_Requests,
				0.0004/1000*float64(stats.Stats.Usage.LocalS3.Read_Requests),
			)
			cost += 0.0004 / 1000 * float64(stats.Stats.Usage.LocalS3.Read_Requests)
			fmt.Fprintf(tw, "  S3 Xfer in[client]\tMB\t%d\t$%.2f\n",
				stats.Stats.Usage.LocalS3.Xfer_In/(1024*1024),
				0.0,
			)
			fmt.Fprintf(tw, "  S3 Xfer out[client]\tMB\t%d\t$%.2f\n",
				stats.Stats.Usage.LocalS3.Xfer_Out/(1024*1024),
				float64(stats.Stats.Usage.LocalS3.Xfer_Out)*0.09/(1024*1024*1024),
			)
			cost += float64(stats.Stats.Usage.LocalS3.Xfer_Out) * 0.09 / (1024 * 1024 * 1024)
			fmt.Fprintf(tw, "  S3 Write requests[remote]\t\t%d\t$%.2f\n",
				stats.Stats.Usage.RemoteS3.Write_Requests,
				0.005/1000*float64(stats.Stats.Usage.RemoteS3.Write_Requests),
			)
			cost += 0.005 / 1000 * float64(stats.Stats.Usage.RemoteS3.Write_Requests)
			fmt.Fprintf(tw, "  S3 Read requests[remote]\t\t%d\t$%.2f\n",
				stats.Stats.Usage.RemoteS3.Read_Requests,
				0.0004/1000*float64(stats.Stats.Usage.RemoteS3.Read_Requests),
			)
			cost += 0.0004 / 1000 * float64(stats.Stats.Usage.RemoteS3.Read_Requests)
			fmt.Fprintf(tw, "  S3 Xfer in[remote]\tMB\t%d\t$%.2f\n",
				stats.Stats.Usage.RemoteS3.Xfer_In/(1024*1024),
				0.0,
			)
			fmt.Fprintf(tw, "  S3 Xfer out[remote]\tMB\t%d\t$%.2f\n",
				stats.Stats.Usage.RemoteS3.Xfer_Out/(1024*1024),
				0.0,
			)
			fmt.Fprintf(tw, "  Total\t$\t\t$%.2f\n",
				cost,
			)
			tw.Flush()
		}
		return subcommands.ExitSuccess
	} else if c.start || c.autostart {
		raiseRlimits()
		if c.detach {
			cmd := exec.Command("/proc/self/exe", "daemon", "-start",
				"-idle-timeout", c.idleTimeout.String(),
				"-path", c.path,
			)
			cmd.SysProcAttr = &syscall.SysProcAttr{
				Setsid: true,
			}
			signal.Ignore(syscall.SIGHUP)
			if err := cmd.Start(); err != nil {
				log.Fatalf("Starting daemon: %s", err.Error())
			}
		} else {
			global := cli.MustState(ctx)
			if err := server.Start(ctx, &server.StartArgs{
				Path:               c.path,
				Session:            global.MustSession(),
				Store:              global.MustStore(),
				IdleTimeout:        c.idleTimeout,
				LlamaCCConcurrency: c.ccConcurrency,
			}); err != nil {
				if c.autostart && err == server.ErrAlreadyRunning {
					return subcommands.ExitSuccess
				}
				log.Fatalf("starting daemon: %s", err)
			}
		}
	} else {
		log.Fatalf("Must pass an action")
	}

	return subcommands.ExitSuccess
}
