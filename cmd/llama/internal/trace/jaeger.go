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

package trace

import (
	encoding_json "encoding/json"
	"fmt"
	"io/ioutil"

	"github.com/jaegertracing/jaeger/model/json"
	"github.com/nelhage/llama/tracing"
)

type jaegerFile struct {
	Data []json.Trace `json:"data"`
}

func treeToJaeger(t *TraceTree) json.Trace {
	pid := json.ProcessID("1")
	out := json.Trace{
		TraceID: json.TraceID(t.span.TraceId),
		Processes: map[json.ProcessID]json.Process{
			pid: {
				ServiceName: "llama",
			},
		},
	}
	t.EachSpan(func(span *tracing.Span) error {
		sp := json.Span{
			TraceID:       json.TraceID(span.TraceId),
			SpanID:        json.SpanID(span.SpanId),
			ParentSpanID:  json.SpanID(span.ParentId),
			OperationName: span.Name,
			StartTime:     uint64(span.Start.UnixNano()) / 1000,
			Duration:      uint64(span.Duration.Microseconds()),
			References: []json.Reference{{
				RefType: json.ChildOf,
				TraceID: json.TraceID(span.TraceId),
				SpanID:  json.SpanID(span.ParentId),
			}},
			ProcessID: json.ProcessID(pid),
		}
		for k, v := range span.Fields {
			kv := json.KeyValue{
				Key:   k,
				Value: v,
			}
			switch v.(type) {
			case string:
				kv.Type = json.StringType
			case bool:
				kv.Type = json.BoolType
			case int64:
				kv.Type = json.Int64Type
			case float64:
				kv.Type = json.Float64Type
			}
			if kv.Type != "" {
				sp.Tags = append(sp.Tags, kv)
			}
		}
		out.Spans = append(out.Spans, sp)

		return nil
	})
	return out
}

func writeJaeger(trees []*TraceTree, path string) error {
	var file jaegerFile
	for _, t := range trees {
		file.Data = append(file.Data, treeToJaeger(t))
	}
	bytes, err := encoding_json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return ioutil.WriteFile(path, bytes, 0644)
}
