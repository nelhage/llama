package store

import "context"

type Store interface {
	Store(ctx context.Context, obj []byte) (string, error)
	Get(ctx context.Context, id string) ([]byte, error)
}
