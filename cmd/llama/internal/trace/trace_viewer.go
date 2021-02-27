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
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/nelhage/llama/tracing"
)

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

func (c *TraceCommand) WriteTraceViewer(spans []tracing.Span, trees []*TraceTree) error {
	fh, err := os.Create(c.traceViewer)
	if err != nil {
		return err
	}
	defer fh.Close()

	var minTs time.Time
	for _, span := range spans {
		if minTs.IsZero() || span.Start.Before(minTs) {
			minTs = span.Start
		}
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
	fmt.Fprintf(fh, "%s\n", out)
	return nil
}
