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
