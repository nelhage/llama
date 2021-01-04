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

package bootstrap

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/google/subcommands"
	"github.com/nelhage/llama/cmd/internal/cli"
)

type BootstrapCommand struct {
	in  *bufio.Reader
	out io.Writer
}

func (*BootstrapCommand) Name() string     { return "bootstrap" }
func (*BootstrapCommand) Synopsis() string { return "Configure Llama and set up AWS resources" }
func (*BootstrapCommand) Usage() string {
	return `bootstrap [flags]
`
}

func (c *BootstrapCommand) SetFlags(flags *flag.FlagSet) {
}

func (c *BootstrapCommand) Execute(ctx context.Context, flag *flag.FlagSet, _ ...interface{}) subcommands.ExitStatus {
	if c.in == nil {
		c.in = bufio.NewReader(os.Stdin)
	}
	if c.out == nil {
		c.out = os.Stdout
	}

	global := cli.MustState(ctx)
	session, err := global.Session()
	if err != nil {
		log.Printf("Unable to configure AWS session: %s", err.Error())
		return subcommands.ExitFailure
	}

	stsSvc := sts.New(session.Copy(aws.NewConfig().WithCredentialsChainVerboseErrors(true)))
	ident, err := stsSvc.GetCallerIdentity(&sts.GetCallerIdentityInput{})
	if err != nil {
		log.Printf("Unable to get AWS account identity information.")
		log.Printf("Do you have AWS credentials configured?")
		return subcommands.ExitFailure
	}

	log.Printf("Configuring llama for AWS account ID %s", *ident.Account)

	if session.Config.Region == nil || *session.Config.Region == "" {
		region, err := c.readRegion(session)
		if err != nil {
			log.Printf("Choosing region: %s", err.Error())
			return subcommands.ExitFailure
		}
		session = session.Copy(aws.NewConfig().WithRegion(region))
		log.Printf("Will configure llama for region: %s\n", region)
	} else {
		log.Printf("Configuring for region: %s [use llama -region REGION bootstrap to override]", *session.Config.Region)
	}

	return subcommands.ExitSuccess
}

func (c *BootstrapCommand) readRegion(sess *session.Session) (string, error) {
	ec2Svc := ec2.New(sess, aws.NewConfig().WithRegion("us-west-2"))
	regions, err := ec2Svc.DescribeRegions(&ec2.DescribeRegionsInput{})
	if err != nil {
		return "", err
	}
	for {
		fmt.Fprintln(c.out, "Which region would you like to use for Llama?")
		for i, r := range regions.Regions {
			fmt.Fprintf(c.out, "[%d] %s\n", i, *r.RegionName)
		}
		fmt.Fprintf(c.out, "> ")
		resp, err := c.in.ReadString('\n')
		if err != nil {
			return "", err
		}
		resp = strings.Trim(resp, " \t\r\n")
		if resp == "" {
			continue
		}
		n, err := strconv.ParseUint(resp, 10, 64)
		if err == nil {
			if int(n) >= len(regions.Regions) {
				continue
			}
			return *regions.Regions[n].RegionName, nil
		}
		return resp, nil
	}

}
