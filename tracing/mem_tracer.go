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

package tracing

import (
	"context"

	"golang.org/x/sync/errgroup"
)

type MemoryTracer struct {
	spans []Span
	ch    chan Span
	wg    *errgroup.Group
}

func (tr *MemoryTracer) writer() error {
	for s := range tr.ch {
		tr.spans = append(tr.spans, s)
	}
	return nil
}

func NewMemoryTracer(ctx context.Context) *MemoryTracer {
	grp, ctx := errgroup.WithContext(ctx)
	tr := &MemoryTracer{
		ch: make(chan Span, bufferSize),
		wg: grp,
	}
	tr.wg.Go(tr.writer)
	return tr
}

func (mt *MemoryTracer) Submit(span *Span) {
	mt.ch <- *span
}

func (mt *MemoryTracer) Close() []Span {
	close(mt.ch)
	mt.wg.Wait()
	return mt.spans
}

func CollectSpans(ctx context.Context, fn func(context.Context) error) ([]Span, error) {
	mt := NewMemoryTracer(ctx)
	ctx = WithTracer(ctx, mt)
	err := fn(ctx)
	spans := mt.Close()
	return spans, err
}
