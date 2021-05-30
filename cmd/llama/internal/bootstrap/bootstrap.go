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
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/google/subcommands"
	"github.com/nelhage/llama/cmd/internal/cli"
)

func stackRolledBack(s *cloudformation.Stack) bool {
	switch *s.StackStatus {
	case cloudformation.StackStatusRollbackComplete,
		cloudformation.StackStatusRollbackFailed,
		cloudformation.StackStatusRollbackInProgress:
		return true
	default:
		return false
	}
}

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

func (c *BootstrapCommand) ensureLlamaCxx() error {
	llamacc, err := exec.LookPath("llamacc")
	if err != nil {
		return err
	}
	llamacxx := path.Join(path.Dir(llamacc), "llamac++")
	_, err = os.Stat(llamacxx)
	if err == nil {
		return nil
	}
	return os.Symlink("llamacc", llamacxx)
}

func (c *BootstrapCommand) Execute(ctx context.Context, flag *flag.FlagSet, _ ...interface{}) subcommands.ExitStatus {
	if c.in == nil {
		c.in = bufio.NewReader(os.Stdin)
	}
	if c.out == nil {
		c.out = os.Stdout
	}

	log.Printf("Ensuring llamac++ symlink exists...")
	err := c.ensureLlamaCxx()
	if err != nil {
		log.Printf("Unable to create llamacc++ symlink. You may need to create it by hand if you want to build C++")
		log.Printf("Error: %s", err.Error())
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
		log.Printf("Unable to get AWS account identity information: %s", err.Error())
		log.Printf("Do you have AWS credentials configured? https://github.com/nelhage/llama#set-up-your-aws-credentials")
		return subcommands.ExitFailure
	}

	log.Printf("AWS credentials detected for account ID %s", *ident.Account)

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

	log.Printf("Creating cloudformation stack...")

	cf := cloudformation.New(session)
	_, err = cf.CreateStack(&cloudformation.CreateStackInput{
		Capabilities: []*string{aws.String(cloudformation.CapabilityCapabilityIam)},
		Parameters:   []*cloudformation.Parameter{},
		TemplateBody: aws.String(CFTemplate),
		StackName:    aws.String("llama"),
	})

	if err != nil {
		if e, ok := err.(awserr.Error); ok && e.Code() == "AlreadyExistsException" {
			log.Printf("The `llama` stack already exists.")
			log.Printf("`llama bootstrap` does not yet support updating the stack.")
			log.Printf("I'm going to proceed assuming it's up-to-date.")
		} else {
			log.Printf("Error creating CF stack: %s", err.Error())
			return subcommands.ExitFailure
		}
	}

	log.Printf("Stack created. Polling until completion...")
	var stack *cloudformation.Stack
poll:
	for {
		describe, err := cf.DescribeStacks(&cloudformation.DescribeStacksInput{
			StackName: aws.String("llama"),
		})
		if err != nil {
			log.Printf("Error describing stack: %s", err.Error())
			continue
		}
		stack = describe.Stacks[0]
		switch *stack.StackStatus {
		case cloudformation.StackStatusCreateComplete:
			break poll
		case cloudformation.StackStatusCreateInProgress:
			time.Sleep(2 * time.Second)
		default:
			if stackRolledBack(stack) {
				log.Printf("Stack is in rollback: %s. Something went wrong.", *stack.StackStatus)
			} else {
				log.Printf("Unknown stack state: %s. Something went wrong.", *stack.StackStatus)
			}
			if stack.StackStatusReason != nil {
				log.Printf("Stack status reason: %s", *stack.StackStatusReason)
			}
			return subcommands.ExitFailure
		}
	}

	log.Printf("Resource creation complete. Writing config...")

	newCfg := *global.Config
	for _, out := range stack.Outputs {
		switch *out.OutputKey {
		case "ObjectStore":
			newCfg.Store = *out.OutputValue
		case "Role":
			newCfg.IAMRole = *out.OutputValue
		case "Repository":
			newCfg.ECRRepository = *out.OutputValue
		}
	}
	newCfg.Region = *session.Config.Region

	cli.WriteConfig(&newCfg, cli.ConfigPath())

	log.Printf("Llama bootstrap complete. You can now create and use Llama functions.")

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
