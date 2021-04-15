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
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/nelhage/llama/tracing"
)

func stringify(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int64:
		return strconv.FormatInt(t, 10)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}

func collectGlobal(tree *TraceTree) map[string]interface{} {
	global := make(map[string]interface{})
	tree.EachSpan(func(span *tracing.Span) error {
		if span.Fields != nil {
			for k, v := range span.Fields {
				if strings.HasPrefix(k, "global.") {
					global[k] = v
				}
			}
		}
		return nil
	})
	return global
}

func treeToCSV(w *csv.Writer, tree *TraceTree, extra []string) {
	var words []string

	global := collectGlobal(tree)

	var walk func(t *TraceTree, path string)
	walk = func(t *TraceTree, path string) {
		if path == "" {
			path = t.span.Name
		} else {
			path = fmt.Sprintf("%s>%s", path, t.span.Name)
		}
		words = append(words[:0],
			t.span.TraceId, t.span.ParentId, t.span.SpanId,
			path, fmt.Sprintf("%d.%09d", t.span.Start.UTC().Unix(), t.span.Start.Nanosecond()),
			strconv.FormatInt(t.span.Duration.Nanoseconds(), 10),
		)
		fields := make(map[string]interface{}, len(global)+len(t.span.Fields))
		for k, v := range global {
			fields[k] = v
		}
		for k, v := range t.span.Fields {
			fields[k] = v
		}
		out, err := json.Marshal(fields)
		if err != nil {
			panic("json marshal")
		}
		words = append(words, string(out))
		for _, col := range extra {
			v := fields[col]
			words = append(words, stringify(v))
		}
		w.Write(words)
		for _, child := range t.children {
			walk(child, path)
		}
	}
	walk(tree, "")
}

func (c *TraceCommand) WriteCSV(spans []tracing.Span, trees []*TraceTree) error {
	fh, err := os.Create(c.csv)
	if err != nil {
		return err
	}
	defer fh.Close()
	w := csv.NewWriter(fh)
	defer w.Flush()

	var extraColumns []string
	if c.csvColumns != "" {
		extraColumns = strings.Split(c.csvColumns, ",")
	}

	headers := append(
		[]string{
			"trace", "parent", "span", "path", "start", "duration_ns", "fields",
		}, extraColumns...)
	w.Write(headers)

	for _, tree := range trees {
		treeToCSV(w, tree, extraColumns)
	}

	return nil
}
