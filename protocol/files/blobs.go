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

package files

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"unicode/utf8"

	"github.com/nelhage/llama/protocol"
	"github.com/nelhage/llama/store"
)

func AppendGet(reqs []store.GetRequest, b *protocol.Blob) []store.GetRequest {
	if b.Ref != "" {
		reqs = append(reqs, store.GetRequest{Id: b.Ref})
	}
	return reqs
}

func ReadBlob(b *protocol.Blob, gets []store.GetRequest) ([]byte, error, []store.GetRequest) {
	if b.Err != "" {
		return nil, errors.New(b.Err), gets
	}
	if b.String != "" {
		return []byte(b.String), nil, gets
	}
	if b.Bytes != nil {
		return b.Bytes, nil, gets
	}
	if b.Ref != "" {
		if gets[0].Id != b.Ref {
			panic(fmt.Sprintf("ReadBlob: bad requests %s != %s", gets[0].Id, b.Ref))
		}
		return gets[0].Data, gets[0].Err, gets[1:]
	}
	return nil, nil, gets
}

func Read(ctx context.Context, st store.Store, b *protocol.Blob) ([]byte, error) {
	gets := AppendGet(nil, b)
	data, err, _ := ReadBlob(b, gets)
	return data, err
}

func FetchFile(f *protocol.File, where string, gets []store.GetRequest) (error, []store.GetRequest) {
	data, err, gets := ReadBlob(&f.Blob, gets)
	if err != nil {
		return err, gets
	}
	mode := f.Mode
	if mode == 0 {
		mode = 0644
	}
	return ioutil.WriteFile(where, data, mode), gets
}

func NewBlob(ctx context.Context, store store.Store, bytes []byte) (*protocol.Blob, error) {
	stringOk := utf8.Valid(bytes)
	if stringOk && len(bytes) < protocol.MaxInlineBlob {
		return &protocol.Blob{String: string(bytes)}, nil
	}
	if base64.StdEncoding.EncodedLen(len(bytes)) < protocol.MaxInlineBlob {
		return &protocol.Blob{Bytes: bytes}, nil
	}
	id, err := store.Store(ctx, bytes)
	if err != nil {
		return nil, err
	}
	return &protocol.Blob{Ref: id}, nil
}

func ReadFile(ctx context.Context, store store.Store, path string) (*protocol.File, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	fi, err := fh.Stat()
	if err != nil {
		return nil, err
	}

	if fi.Mode().IsDir() {
		return nil, errors.New("ReadFile: got directory")
	}
	bytes, err := ioutil.ReadAll(fh)
	if err != nil {
		return nil, err
	}
	blob, err := NewBlob(ctx, store, bytes)
	if err != nil {
		return nil, err
	}
	return &protocol.File{
		Blob: *blob,
		Mode: fi.Mode(),
	}, nil
}
