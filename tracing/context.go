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
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

type key int

const (
	tracerKey key = iota
	spanKey
)

func WithTracer(ctx context.Context, tr Tracer) context.Context {
	return context.WithValue(ctx, tracerKey, tr)
}

func TracerFromContext(ctx context.Context) (Tracer, bool) {
	v, ok := ctx.Value(tracerKey).(Tracer)
	return v, ok
}

func WithSpan(ctx context.Context, span *Span) context.Context {
	return context.WithValue(ctx, spanKey, span)
}

func SpanFromContext(ctx context.Context) (*Span, bool) {
	v, ok := ctx.Value(spanKey).(*Span)
	return v, ok
}

func PropagationFromContext(ctx context.Context) *Propagation {
	span, ok := SpanFromContext(ctx)
	if !ok {
		return nil
	}
	return &Propagation{
		TraceId:  span.TraceId,
		ParentId: span.SpanId,
	}
}

func StartSpan(ctx context.Context, name string) (context.Context, *SpanBuilder) {
	parent, ok := SpanFromContext(ctx)
	if ok {
		return StartSpanInTrace(ctx, name, parent.TraceId, parent.SpanId)
	} else {
		return StartSpanInTrace(ctx, name, newId(), "")
	}
}

func StartPropagatedSpan(ctx context.Context, name string, p *Propagation) (context.Context, *SpanBuilder) {
	if p == nil {
		return StartSpan(ctx, name)
	} else {
		return StartSpanInTrace(ctx, name, p.TraceId, p.ParentId)
	}
}

func StartSpanInTrace(ctx context.Context, name, trace, parent string) (context.Context, *SpanBuilder) {
	sb := SpanBuilder{
		span: Span{
			SpanId:   newId(),
			TraceId:  trace,
			ParentId: parent,
			Name:     name,
			Start:    time.Now(),
		},
	}
	sb.tracer, _ = TracerFromContext(ctx)
	return WithSpan(ctx, &sb.span), &sb
}

func SubmitAll(ctx context.Context, spans []Span) {
	tracer, ok := TracerFromContext(ctx)
	if ok {
		for _, span := range spans {
			tracer.Submit(&span)
		}
	}
}

func newId() string {
	var buf [8]byte
	if _, err := rand.Reader.Read(buf[:]); err != nil {
		panic(fmt.Sprintf("rand: %s", err.Error()))
	}
	return hex.EncodeToString(buf[:])
}
