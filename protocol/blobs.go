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

package protocol

import (
	"context"
	"encoding/base64"
	"errors"
	"io/ioutil"
	"os"
	"unicode/utf8"

	"github.com/nelhage/llama/store"
)

const MaxInlineBlob = 10 * 1024

type Blob struct {
	String string `json:"s,omitempty"`
	Bytes  []byte `json:"b,omitempty"`
	Ref    string `json:"r,omitempty"`
	Err    string `json:"e,omitempty"`
}

type File struct {
	Blob
	Mode os.FileMode `json:"m,omitempty"`
}

func (b *Blob) Read(ctx context.Context, store store.Store) ([]byte, error) {
	if b.String != "" {
		return []byte(b.String), nil
	}
	if b.Bytes != nil {
		return b.Bytes, nil
	}
	if b.Ref != "" {
		return store.Get(ctx, b.Ref)
	}
	return nil, nil
}

func (f *File) Fetch(ctx context.Context, store store.Store, where string) error {
	if f.Err != "" {
		return errors.New(f.Err)
	}
	data, err := f.Read(ctx, store)
	if err != nil {
		return err
	}
	if f.Mode.IsDir() {
		return errors.New("is directory")
	}
	return ioutil.WriteFile(where, data, f.Mode)
}

func NewBlob(ctx context.Context, store store.Store, bytes []byte) (*Blob, error) {
	stringOk := utf8.Valid(bytes)
	if stringOk && len(bytes) < MaxInlineBlob {
		return &Blob{String: string(bytes)}, nil
	}
	if base64.StdEncoding.EncodedLen(len(bytes)) < MaxInlineBlob {
		return &Blob{Bytes: bytes}, nil
	}
	id, err := store.Store(ctx, bytes)
	if err != nil {
		return nil, err
	}
	return &Blob{Ref: id}, nil
}

func ReadFile(ctx context.Context, store store.Store, path string) (*File, error) {
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
	return &File{
		Blob: *blob,
		Mode: fi.Mode(),
	}, nil
}
