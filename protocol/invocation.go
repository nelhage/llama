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

package protocol

import (
	"time"

	"github.com/nelhage/llama/tracing"
)

type InvocationSpec struct {
	Trace   *tracing.Propagation `json:"trace,omitemptry"`
	Args    []string             `json:"args"`
	Stdin   *Blob                `json:"stdin,omitempty"`
	Files   FileList             `json:"files,omitempty"`
	Outputs []string             `json:"outputs,emitempty"`
}

type InvocationResponse struct {
	ExitStatus  int            `json:"status"`
	Stdout      *Blob          `json:"stdout,omitempty"`
	Stderr      *Blob          `json:"stderr,omitempty"`
	Outputs     FileList       `json:"outputs,omitempty"`
	InlineSpans []tracing.Span `json:"inlinespans,omitempty"`
	Spans       *Blob          `json:"spans,omitempty"`
	Usage       UsageMetrics   `json:"usage"`
	Times       Timing         `json:"times"`
}

type StoreUsage struct {
	Write_Requests uint64
	Read_Requests  uint64
	Xfer_In        uint64
	Xfer_Out       uint64
}

type LambdaUsage struct {
	Millis    uint64
	MB_Millis uint64
	Requests  uint64
}

type UsageMetrics struct {
	Lambda LambdaUsage
	S3     StoreUsage
}

type Timing struct {
	ColdStart bool          `json:"cold"`
	E2E       time.Duration `json:"e2e"`
	Fetch     time.Duration `json:"fetch"`
	Upload    time.Duration `json:"upload"`
	Exec      time.Duration `json:"exec"`
}
