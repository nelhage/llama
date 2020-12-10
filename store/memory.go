package store

import (
	"context"
	"encoding/hex"
	"errors"

	"golang.org/x/crypto/blake2b"
)

type inMemory struct {
	objects map[string][]byte
}

func (s *inMemory) Store(ctx context.Context, obj []byte) (string, error) {
	sha := blake2b.Sum256(obj)
	id := hex.EncodeToString(sha[:])
	s.objects[id] = append([]byte(nil), obj...)
	return id, nil
}

func (s *inMemory) Get(ctx context.Context, id string) ([]byte, error) {
	if got, ok := s.objects[id]; ok {
		return append([]byte(nil), got...), nil
	}
	return nil, errors.New("no such object")
}

func InMemory() Store {
	return &inMemory{
		objects: make(map[string][]byte),
	}
}
