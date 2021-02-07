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
	"encoding/json"
	"io"

	"golang.org/x/sync/errgroup"
)

type WriterTracer struct {
	ctx context.Context
	w   io.Writer
	ch  chan Span
	wg  *errgroup.Group
}

func (wt *WriterTracer) Submit(span *Span) {
	select {
	case <-wt.ctx.Done():
	case wt.ch <- *span:
	}
}

func (wt *WriterTracer) Close() error {
	close(wt.ch)
	err := wt.wg.Wait()
	if cl, ok := wt.w.(io.WriteCloser); ok {
		cl.Close()
	}
	return err
}

func (wt *WriterTracer) writer(ctx context.Context) error {
	encoder := json.NewEncoder(wt.w)
	for {
		select {
		case span, ok := <-wt.ch:
			if !ok {
				return nil
			}
			if err := encoder.Encode(&span); err != nil {
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

const bufferSize = 10

func WithWriterTracer(ctx context.Context, w io.Writer) (context.Context, *WriterTracer) {
	wg, ctx := errgroup.WithContext(ctx)
	wt := &WriterTracer{
		ctx: ctx,
		wg:  wg,
		w:   w,
		ch:  make(chan Span, bufferSize),
	}
	wt.wg.Go(func() error { return wt.writer(ctx) })
	return WithTracer(ctx, wt), wt
}

func TraceToWriter(ctx context.Context, w io.Writer, fn func(context.Context) error) error {
	ctx, wt := WithWriterTracer(ctx, w)
	defer wt.Close()
	err := fn(ctx)
	return err
}
