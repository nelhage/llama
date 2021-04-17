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
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/google/subcommands"
	"github.com/klauspost/compress/zstd"
	"github.com/nelhage/llama/tracing"
)

type TraceCommand struct {
	zstd        bool
	fixup       bool
	maxTrees    int
	depth       int
	csv         string
	csvColumns  string
	traceViewer string
	trace       string
	jaeger      string
	addFields   string

	parquet string
}

func (*TraceCommand) Name() string     { return "trace" }
func (*TraceCommand) Synopsis() string { return "Manipulate llama trace files" }
func (*TraceCommand) Usage() string {
	return `trace OPTIONS file.trace
`
}
func (c *TraceCommand) SetFlags(flags *flag.FlagSet) {
	flags.BoolVar(&c.zstd, "zstd", false, "Read zstd-compressed trace files")
	flags.BoolVar(&c.fixup, "fixup", false, "Attempt to fix-up span timestamps to be internally consistent")
	flags.IntVar(&c.maxTrees, "max-trees", 0, "Render only the first N trees")
	flags.IntVar(&c.depth, "depth", 0, "Render the trace tree only to depth N")
	flags.StringVar(&c.trace, "trace", "", "Only examine specified trace")

	flags.StringVar(&c.addFields, "add-fields", "", "Extra fields to add to traces, in comma-separated K=V format")

	flags.StringVar(&c.csv, "csv", "", "Write annotated spans to CSV")
	flags.StringVar(&c.csvColumns, "csv-columns", "", "Extra fields to explode into CSV columns")

	flags.StringVar(&c.traceViewer, "trace-viewer", "", "Write out in Chrome trace-viewer format")
	flags.StringVar(&c.jaeger, "jaeger", "", "Write out in jaeger JSON format")

	flags.StringVar(&c.parquet, "parquet", "", "Write spans as a parquet file")
}

type TraceTree struct {
	span     *tracing.Span
	children []*TraceTree
}

func (t *TraceTree) EachSpan(cb func(span *tracing.Span) error) error {
	if err := cb(t.span); err != nil {
		return err
	}
	for _, ch := range t.children {
		if err := ch.EachSpan(cb); err != nil {
			return err
		}
	}
	return nil
}

func buildTrees(spans []tracing.Span) []*TraceTree {
	var trees []*TraceTree
	bySpan := make(map[string]*TraceTree)
	for i := len(spans) - 1; i >= 0; i-- {
		span := &spans[i]
		me, ok := bySpan[span.SpanId]
		if ok {
			me.span = span
		} else {
			me = &TraceTree{span: span}
			bySpan[span.SpanId] = me
		}

		if span.ParentId == "" {
			trees = append(trees, me)
			continue
		}
		if par, ok := bySpan[span.ParentId]; ok {
			par.children = append(par.children, me)
		} else {
			log.Printf("missing parent: span=%s parent=%s", span.SpanId, span.ParentId)
		}
	}
	return trees
}

type walker struct {
	events []Event
	start  time.Time
	pid    int
	tid    int
}

func (w *walker) walk(tree *TraceTree, maxDepth int) {
	if maxDepth == 0 {
		return
	}

	if tree.span.Start.Before(w.start) {
		panic(fmt.Sprintf("out of order span=%v %s < %s", tree.span, tree.span.Start, w.start))
	}

	if tree.span.Fields == nil {
		tree.span.Fields = make(map[string]interface{})
	}
	tree.span.Fields["span_id"] = tree.span.SpanId

	w.events = append(w.events, Event{
		Pid:  w.pid,
		Tid:  w.tid,
		Ph:   "b",
		Cat:  "trace",
		Id:   w.tid,
		Ts:   tree.span.Start.Sub(w.start).Microseconds(),
		Args: tree.span.Fields,
		Name: tree.span.Name,
	})
	for _, ch := range tree.children {
		w.walk(ch, maxDepth-1)
	}
	w.events = append(w.events, Event{
		Pid:  w.pid,
		Tid:  w.tid,
		Ph:   "e",
		Cat:  "trace",
		Id:   w.tid,
		Ts:   (tree.span.Start.Sub(w.start) + tree.span.Duration).Microseconds(),
		Args: tree.span.Fields,
		Name: tree.span.Name,
	})
}

func fixupSpans(t *TraceTree) {
	fixupBounds(t, t.span.Start, t.span.Start.Add(t.span.Duration), 0)
}

func fixupBounds(tree *TraceTree, min, max time.Time, correction time.Duration) {
	tree.span.Start = tree.span.Start.Add(correction)
	if tree.span.Start.Before(min) {
		delta := min.Sub(tree.span.Start)
		log.Printf("span starts before parent id=%s parent=%s d=%s start=%s parent_start=%s",
			tree.span.SpanId, tree.span.ParentId, delta, tree.span.Start, min)
		correction += delta
		tree.span.Start = min
	}
	end := tree.span.Start.Add(tree.span.Duration)
	if max.Before(end) {
		log.Printf("span ends after parent id=%s parent=%s d=%s end=%s parent_end=%s",
			tree.span.SpanId, tree.span.ParentId, end.Sub(max), end, max)
		delta := end.Sub(max)
		if tree.span.Start.Add(-delta).After(min) {
			correction -= delta
			tree.span.Start = tree.span.Start.Add(-delta)
			end = tree.span.Start.Add(tree.span.Duration)
		} else {
			tree.span.Duration = max.Sub(tree.span.Start)
			end = max
		}
	}
	for _, ch := range tree.children {
		fixupBounds(ch, tree.span.Start, end, correction)
	}
}

func (c *TraceCommand) Execute(ctx context.Context, flag *flag.FlagSet, _ ...interface{}) subcommands.ExitStatus {
	if c.depth == 0 {
		c.depth = 1 << 24
	}
	fname := flag.Arg(0)
	fh, err := os.Open(fname)
	if err != nil {
		log.Fatalf("open(%q): %s", fname, err)
	}
	defer fh.Close()
	var r io.Reader = fh
	if c.zstd {
		dec, err := zstd.NewReader(r)
		if err != nil {
			log.Fatalf("zstd: %s", err.Error())
		}
		defer dec.Close()
		r = dec
	}
	var extraFields map[string]string
	if c.addFields != "" {
		extraFields = make(map[string]string)
		for _, kv := range strings.Split(c.addFields, ",") {
			eq := strings.IndexRune(kv, '=')
			if eq < 0 {
				log.Printf("-add-fields: bad value %q", kv)
				return subcommands.ExitUsageError
			}
			extraFields[kv[:eq]] = kv[eq+1:]
		}
	}
	decoder := json.NewDecoder(r)
	var spans []tracing.Span
	for {
		var span tracing.Span
		err := decoder.Decode(&span)
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("read json: %s", err.Error())
		}
		if span.SpanId == "" {
			log.Printf("skipping bad span (n=%d): %v", len(spans), span)
			continue
		}
		if c.trace != "" && span.TraceId != c.trace {
			continue
		}
		if extraFields != nil {
			if span.Fields == nil {
				span.Fields = make(map[string]interface{})
			}
			for k, v := range extraFields {
				span.Fields[k] = v
			}
		}
		spans = append(spans, span)
	}
	trees := buildTrees(spans)
	if c.fixup {
		for _, t := range trees {
			fixupSpans(t)
		}
	}
	log.Printf("built %d trace trees", len(trees))
	sort.Slice(trees, func(i, j int) bool { return trees[i].span.Start.Before(trees[j].span.Start) })
	if c.maxTrees > 0 && len(trees) > c.maxTrees {
		trees = trees[:c.maxTrees]
	}

	if c.traceViewer != "" {
		err := c.WriteTraceViewer(spans, trees)
		if err != nil {
			log.Fatalf("trace viewer: %s", err.Error())
		}
	}

	if c.csv != "" {
		err := c.WriteCSV(spans, trees)
		if err != nil {
			log.Fatalf("write csv: %s", err.Error())
		}
	}

	if c.jaeger != "" {
		err := writeJaeger(trees, c.jaeger)
		if err != nil {
			log.Fatalf("write jaeger: %s", err.Error())
		}
	}

	if c.parquet != "" {
		err := c.WriteParquet(spans, trees)
		if err != nil {
			log.Fatalf("write parquet: %s", err.Error())
		}
	}

	return subcommands.ExitFailure
}
