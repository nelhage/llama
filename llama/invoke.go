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

package llama

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/nelhage/llama/protocol"
	"github.com/nelhage/llama/tracing"
)

type InvokeArgs struct {
	Function   string
	ReturnLogs bool
	Spec       protocol.InvocationSpec
}

type InvokeResult struct {
	Logs     []byte
	Response protocol.InvocationResponse
}

type ErrorReturn struct {
	Payload []byte
	Logs    []byte
}

func (e *ErrorReturn) Error() string {
	return fmt.Sprintf("Function returned error: %q", e.Payload)
}

func Invoke(ctx context.Context, svc *lambda.Lambda, args *InvokeArgs) (*InvokeResult, error) {
	ctx, span := tracing.StartSpan(ctx, "llama.Invoke")
	defer span.End()
	span.SetLabel("function", args.Function)

	if span.WillSubmit() {
		args.Spec.Trace = span.Propagation()
	}

	payload, err := json.Marshal(&args.Spec)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	span.SetMetric("payload_bytes", float64(len(payload)))

	input := lambda.InvokeInput{
		FunctionName: &args.Function,
		Payload:      payload,
	}
	if args.ReturnLogs {
		input.LogType = aws.String(lambda.LogTypeTail)
	}

	var out InvokeResult

	resp, err := svc.Invoke(&input)
	if err != nil {
		return nil, fmt.Errorf("Invoke(): %w", err)
	}
	if resp.LogResult != nil {
		logs, _ := base64.StdEncoding.DecodeString(*resp.LogResult)
		out.Logs = logs
	}
	if resp.FunctionError != nil {
		return nil, &ErrorReturn{
			Payload: resp.Payload,
			Logs:    out.Logs,
		}
	}

	span.SetMetric("response_bytes", float64(len(resp.Payload)))

	if err := json.Unmarshal(resp.Payload, &out.Response); err != nil {
		return nil, fmt.Errorf("unmarshal: %q", err)
	}

	tracing.SubmitAll(ctx, out.Response.Spans)

	span.SetMetric("e2e_ms", float64(out.Response.Times.E2E.Milliseconds()))
	span.SetMetric("fetch_ms", float64(out.Response.Times.Fetch.Milliseconds()))
	span.SetMetric("exec_ms", float64(out.Response.Times.Exec.Milliseconds()))
	span.SetMetric("upload_ms", float64(out.Response.Times.Upload.Milliseconds()))
	if out.Response.Times.ColdStart {
		span.SetLabel("cold_start", "true")
	}

	return &out, nil
}
