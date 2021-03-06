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

type Span struct {
	TraceId  string `json:"trace_id"`
	SpanId   string `json:"span_id"`
	ParentId string `json:"parent_id"`

	Name     string                 `json:"name"`
	Start    time.Time              `json:"start"`
	Duration time.Duration          `json:"duration"`
	Fields   map[string]interface{} `json:"fields"`
}

type Propagation struct {
	TraceId  string `json:"trace_id"`
	ParentId string `json:"parent_id"`
}
