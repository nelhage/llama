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

package cli

import (
	"context"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/nelhage/llama/store"
)

type GlobalState struct {
	Session     *session.Session
	Region      string
	ObjectStore string

	Store store.Store
}

type key int

var globalKey key

func WithState(ctx context.Context, state *GlobalState) context.Context {
	return context.WithValue(ctx, globalKey, state)
}

func GetState(ctx context.Context) (*GlobalState, bool) {
	v, ok := ctx.Value(globalKey).(*GlobalState)
	return v, ok
}

func MustState(ctx context.Context) *GlobalState {
	state, ok := GetState(ctx)
	if !ok {
		panic("MustState: no CLI state present")
	}
	return state
}
