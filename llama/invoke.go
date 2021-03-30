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
	"log"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/golang/snappy"
	"github.com/nelhage/llama/protocol"
	"github.com/nelhage/llama/protocol/files"
	"github.com/nelhage/llama/store"
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

func Invoke(ctx context.Context, svc *lambda.Lambda,
	st store.Store, args *InvokeArgs) (*InvokeResult, error) {
	ctx, span := tracing.StartSpan(ctx, "llama.Invoke")
	defer span.End()
	span.AddField("function", args.Function)

	if span.WillSubmit() {
		args.Spec.Trace = span.Propagation()
	}

	payload, err := json.Marshal(&args.Spec)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	span.AddField("payload_bytes", len(payload))

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

	span.AddField("response_bytes", len(resp.Payload))

	if err := json.Unmarshal(resp.Payload, &out.Response); err != nil {
		return nil, fmt.Errorf("unmarshal: %q", err)
	}

	if out.Response.Spans != nil {
		gets := files.AppendGet(nil, out.Response.Spans)
		st.GetObjects(ctx, gets)
		spandata, err, _ := files.ReadBlob(out.Response.Spans, gets)
		if err == nil {
			spandata, err = snappy.Decode(nil, spandata)
		}
		if err != nil {
			log.Printf("error receiving traces: %s", err.Error())
		} else {
			var spans []tracing.Span
			if json.Unmarshal(spandata, &spans) == nil {
				tracing.SubmitAll(ctx, spans)
			}
		}
	}
	if out.Response.InlineSpans != nil {
		tracing.SubmitAll(ctx, out.Response.InlineSpans)
	}

	span.AddField("e2e_ms", out.Response.Times.E2E.Milliseconds())
	span.AddField("fetch_ms", out.Response.Times.Fetch.Milliseconds())
	span.AddField("exec_ms", out.Response.Times.Exec.Milliseconds())
	span.AddField("upload_ms", out.Response.Times.Upload.Milliseconds())
	if out.Response.Times.ColdStart {
		span.AddField("cold_start", true)
	}

	return &out, nil
}
