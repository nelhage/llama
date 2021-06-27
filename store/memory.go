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

import (
	"context"
	"encoding/hex"

	"github.com/nelhage/llama/protocol"
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

func (s *inMemory) GetObjects(ctx context.Context, gets []GetRequest) {
	for i := range gets {
		id := gets[i].Id
		if got, ok := s.objects[id]; ok {
			gets[i].Data = append([]byte(nil), got...)
		} else {
			gets[i].Err = ErrNotExists
		}
	}
}

func (s *inMemory) FetchAWSUsage(u *protocol.StoreUsage) {}

func InMemory() Store {
	return &inMemory{
		objects: make(map[string][]byte),
	}
}
