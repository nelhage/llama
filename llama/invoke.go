package llama

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"runtime/trace"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/nelhage/llama/protocol"
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
}

func (e *ErrorReturn) Error() string {
	return fmt.Sprintf("Function returned error: %q", e.Payload)
}

func Invoke(ctx context.Context, svc *lambda.Lambda, args *InvokeArgs) (*InvokeResult, error) {
	payload, err := json.Marshal(&args.Spec)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	input := lambda.InvokeInput{
		FunctionName: &args.Function,
		Payload:      payload,
	}
	if args.ReturnLogs {
		input.LogType = aws.String(lambda.LogTypeTail)
	}

	var out InvokeResult

	var resp *lambda.InvokeOutput
	trace.WithRegion(ctx, "Invoke", func() {
		resp, err = svc.Invoke(&input)
	})
	if err != nil {
		return nil, fmt.Errorf("Invoke(): %w", err)
	}
	if resp.FunctionError != nil {
		return nil, &ErrorReturn{resp.Payload}
	}

	if resp.LogResult != nil {
		logs, _ := base64.StdEncoding.DecodeString(*resp.LogResult)
		out.Logs = logs
	}

	if err := json.Unmarshal(resp.Payload, &out.Response); err != nil {
		return nil, fmt.Errorf("unmarshal: %q", err)
	}

	return &out, nil
}
