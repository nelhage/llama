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

func (sp *SpanBuilder) ensureMetrics() {
	if sp.span.Metrics == nil {
		sp.span.Metrics = make(map[string]float64)
	}
}

func (sp *SpanBuilder) ensureLabels() {
	if sp.span.Labels == nil {
		sp.span.Labels = make(map[string]string)
	}
}

func (sp *SpanBuilder) SetMetric(name string, v float64) {
	sp.ensureMetrics()
	sp.span.Metrics[name] = v
}

func (sp *SpanBuilder) IncMetric(name string, delta float64) {
	sp.ensureMetrics()
	sp.span.Metrics[name] += delta
}

func (sp *SpanBuilder) SetLabel(name string, value string) {
	sp.ensureLabels()
	sp.span.Labels[name] = value
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
