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
	"errors"

	"github.com/nelhage/llama/protocol"
)

type GetRequest struct {
	Id   string
	Data []byte
	Err  error
}

var ErrNotExists = errors.New("Requested object does not exist")

type Store interface {
	Store(ctx context.Context, obj []byte) (string, error)
	GetObjects(ctx context.Context, gets []GetRequest)
	FetchAWSUsage(u *protocol.StoreUsage)
}

func Get(ctx context.Context, st Store, id string) ([]byte, error) {
	gets := []GetRequest{{Id: id}}
	st.GetObjects(ctx, gets)
	return gets[0].Data, gets[0].Err
}
