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

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"time"

	"github.com/google/subcommands"
	"github.com/nelhage/llama/tracing"
)

type TraceCommand struct {
	maxTrees int
	depth    int
}

func (*TraceCommand) Name() string     { return "trace" }
func (*TraceCommand) Synopsis() string { return "Manipulate llama trace files" }
func (*TraceCommand) Usage() string {
	return `trace file.trace]
`
}

func (c *TraceCommand) SetFlags(flags *flag.FlagSet) {
	flags.IntVar(&c.maxTrees, "max-trees", 0, "Render only the first N trees")
	flags.IntVar(&c.depth, "depth", 0, "Render the trace tree only to depth N")
}

type Event struct {
	Pid  int    `json:"pid"`
	Tid  int    `json:"tid"`
	Ph   string `json:"ph,omitempty"`
	Name string `json:"name,omitempty"`

	Cat  string                 `json:"cat,omitempty"`
	Id   int                    `json:"id,omitempty"`
	Ts   int64                  `json:"ts"`
	Dur  int64                  `json:"dur,omitempty"`
	Args map[string]interface{} `json:"args,omitempty"`
}

type TraceTree struct {
	span     *tracing.Span
	children []*TraceTree
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
			log.Printf("missing parent: %s", span.ParentId)
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
		// log.Printf("span starts before parent id=%s parent=%s d=%s start=%s parent_start=%s",
		//  tree.span.SpanId, tree.span.ParentId, delta, tree.span.Start, min)
		correction += delta
		tree.span.Start = min
	}
	end := tree.span.Start.Add(tree.span.Duration)
	if max.Before(end) {
		// log.Printf("span ends after parent id=%s parent=%s d=%s end=%s parent_end=%s",
		//  tree.span.SpanId, tree.span.ParentId, end.Sub(max), end, max)
		delta := end.Sub(max)
		if tree.span.Start.Add(-delta).After(min) {
			correction -= delta
			tree.span.Start = tree.span.Start.Add(-delta)
			end = tree.span.Start.Add(tree.span.Duration)
		} else {
			log.Printf("child is longer than parent span=%s?", tree.span.SpanId)
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
	decoder := json.NewDecoder(fh)
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
		spans = append(spans, span)
	}

	var minTs time.Time
	for _, span := range spans {
		if minTs.IsZero() || span.Start.Before(minTs) {
			minTs = span.Start
		}
	}

	trees := buildTrees(spans)
	for _, t := range trees {
		fixupSpans(t)
	}

	log.Printf("built %d trace trees", len(trees))
	sort.Slice(trees, func(i, j int) bool { return trees[i].span.Start.Before(trees[j].span.Start) })
	if c.maxTrees > 0 && len(trees) > c.maxTrees {
		trees = trees[:c.maxTrees]
	}
	var events []Event
	for i, tree := range trees {
		w := walker{
			start:  minTs,
			pid:    1,
			tid:    1 + i,
			events: events,
		}
		w.events = append(w.events, Event{
			Pid:  w.pid,
			Tid:  w.tid,
			Ph:   "X",
			Ts:   tree.span.Start.Sub(minTs).Microseconds(),
			Dur:  tree.span.Duration.Microseconds(),
			Name: tree.span.Name,
		})
		w.walk(tree, c.depth)
		events = w.events
	}

	out, err := json.MarshalIndent(&events, "", "  ")
	if err != nil {
		log.Fatalf("marshal: %v", err)
	}
	fmt.Printf("%s\n", out)

	return subcommands.ExitFailure
}
