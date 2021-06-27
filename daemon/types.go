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

package daemon

import (
	"time"

	"github.com/nelhage/llama/files"
	"github.com/nelhage/llama/protocol"
	"github.com/nelhage/llama/tracing"
)

type PingArgs struct{}
type PingReply struct {
	ServerPid int
}

type ShutdownArgs struct{}
type ShutdownReply struct{}

type InvokeWithFilesArgs struct {
	Trace      *tracing.Propagation
	Function   string
	ReturnLogs bool
	Args       []string
	Stdin      []byte
	Files      files.List
	Outputs    files.List

	// If true, release the llamacc semaphore to allow other
	// llamacc processes to use CPU while we talk to AWS
	DropSemaphore bool
}

type InvokeWithFilesReply struct {
	InvokeErr  string
	ExitStatus int
	Stdout     []byte
	Stderr     []byte
	Logs       []byte

	Timing Timing
}

type Timing struct {
	E2E    time.Duration
	Upload time.Duration
	Fetch  time.Duration
	Remote protocol.Timing
	Invoke time.Duration
}

type Stats struct {
	InFlight    uint64
	MaxInFlight uint64

	Invocations    uint64
	FunctionErrors uint64
	OtherErrors    uint64
	ExitStatuses   [256]uint64

	Usage AWSUsage
}

type AWSUsage struct {
	Lambda   protocol.LambdaUsage
	LocalS3  protocol.StoreUsage
	RemoteS3 protocol.StoreUsage
}

type StatsArgs struct {
	Reset bool
}
type StatsReply struct {
	Stats Stats
}

type TraceSpansArgs struct {
	Spans []tracing.Span
}

type TraceSpansReply struct{}

type GetCompilerIncludePathArgs struct {
	Compiler string
	Language string
}

type GetCompilerIncludePathReply struct {
	Paths []string
}
