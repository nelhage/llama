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

import "time"

type Tracer interface {
	Submit(span *Span)
}

type SpanBuilder struct {
	tracer Tracer
	span   Span
}

func (sp *SpanBuilder) ensureFields() {
	if sp.span.Fields == nil {
		sp.span.Fields = make(map[string]interface{})
	}
}

func (sp *SpanBuilder) AddField(name string, v interface{}) {
	sp.ensureFields()
	sp.span.Fields[name] = v
}

func (sp *SpanBuilder) End() *Span {
	sp.span.Duration = time.Since(sp.span.Start)
	if sp.tracer != nil {
		sp.tracer.Submit(&sp.span)
	}
	return &sp.span
}

func (sp *SpanBuilder) TraceId() string {
	return sp.span.TraceId
}

func (sp *SpanBuilder) Id() string {
	return sp.span.SpanId
}

func (sp *SpanBuilder) WillSubmit() bool {
	return sp.tracer != nil
}

func (sp *SpanBuilder) Propagation() *Propagation {
	return &Propagation{
		TraceId:  sp.span.TraceId,
		ParentId: sp.span.SpanId,
	}
}
