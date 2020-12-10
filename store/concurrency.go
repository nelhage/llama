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

package store

import "context"

type concurrencyLimitedStore struct {
	inner  Store
	tokens chan struct{}
}

func LimitConcurrency(store Store, concurrency int) Store {
	tokens := make(chan struct{}, concurrency)
	for i := 0; i < concurrency; i++ {
		tokens <- struct{}{}
	}
	return &concurrencyLimitedStore{
		inner:  store,
		tokens: tokens,
	}
}

func (s *concurrencyLimitedStore) acquireToken() {
	<-s.tokens
}

func (s *concurrencyLimitedStore) releaseToken() {
	s.tokens <- struct{}{}
}

func (s *concurrencyLimitedStore) Store(ctx context.Context, obj []byte) (string, error) {
	s.acquireToken()
	defer s.releaseToken()
	return s.inner.Store(ctx, obj)
}

func (s *concurrencyLimitedStore) Get(ctx context.Context, id string) ([]byte, error) {
	s.acquireToken()
	defer s.releaseToken()
	return s.inner.Get(ctx, id)
}
