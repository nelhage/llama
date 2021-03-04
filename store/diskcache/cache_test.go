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

package diskcache

import (
	"crypto/rand"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/nelhage/llama/store/internal/storeutil"
	"github.com/stretchr/testify/assert"
)

const (
	fileA = "test file A\n"
	fileB = "file b\n"
)

func TestCache(t *testing.T) {
	cache := New(t.TempDir(), 1024*1024)

	idA := storeutil.HashObject([]byte(fileA))

	got, ok := cache.Get(idA)
	assert.False(t, ok)
	assert.Nil(t, got)

	cache.Put(idA, []byte(fileA))
	got, ok = cache.Get(idA)
	assert.True(t, ok)
	assert.Equal(t, got, []byte(fileA))

	idB := storeutil.HashObject([]byte(fileB))
	got, ok = cache.Get(idB)
	assert.False(t, ok)
	assert.Nil(t, got)

	cache.Put(idB, []byte(fileB))
	got, ok = cache.Get(idA)
	assert.True(t, ok)
	assert.Equal(t, got, []byte(fileA))

	got, ok = cache.Get(idB)
	assert.True(t, ok)
	assert.Equal(t, got, []byte(fileB))
}

func TestEviction(t *testing.T) {
	cache := New(t.TempDir(), 1024)

	smallObjs := [][]byte{
		[]byte("a"),
		[]byte("b"),
		[]byte("c"),
	}
	var smallIds []string
	for _, o := range smallObjs {
		id := storeutil.HashObject(o)
		smallIds = append(smallIds, id)
		cache.Put(id, o)
	}

	assert.Equal(t, 3, len(cache.objects.have))

	bigObject := make([]byte, 1024-5-len(smallIds[0]))
	rand.Reader.Read(bigObject)

	bigId := storeutil.HashObject(bigObject)

	cache.Put(bigId, bigObject)
	assert.Equal(t, 1, len(cache.objects.have))
	assert.NotNil(t, cache.objects.have[bigId])
	assert.LessOrEqual(t, cache.objects.bytes, uint64(1024))

	tooBig := append(bigObject, bigObject...)
	tooBigId := storeutil.HashObject(tooBig)
	cache.Put(tooBigId, tooBig)

	got, ok := cache.Get(tooBigId)
	assert.Nil(t, got)
	assert.False(t, ok)
	assert.Equal(t, 0, len(cache.objects.have))
	filepath.Walk(cache.root, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			t.Fatalf("unexpected file in cache directory %q", path)
		}
		return nil
	})
}
