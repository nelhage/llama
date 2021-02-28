package diskcache

import (
	"context"
	"crypto/rand"
	"io/fs"
	"log"
	"path/filepath"
	"testing"

	"github.com/nelhage/llama/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type loggingStore struct {
	inner store.Store
	gets  []string
}

func (l *loggingStore) Store(ctx context.Context, data []byte) (string, error) {
	return l.inner.Store(ctx, data)
}

func (l *loggingStore) GetObjects(ctx context.Context, gets []store.GetRequest) {
	for i := range gets {
		l.gets = append(l.gets, gets[i].Id)
	}
	l.inner.GetObjects(ctx, gets)
}

const (
	fileA = "test file A\n"
	fileB = "file b\n"
	fileC = "yet another file yo\n"
)

func TestCache(t *testing.T) {
	ctx := context.Background()

	mem := &loggingStore{inner: store.InMemory()}
	cache := New(mem, t.TempDir(), 1024*1024)

	idA, err := mem.Store(ctx, []byte(fileA))
	assert.NoError(t, err, "put a")

	gets := []store.GetRequest{{Id: idA}}
	cache.GetObjects(ctx, gets)
	assert.NoError(t, gets[0].Err)
	assert.Equal(t, gets[0].Data, []byte(fileA))

	assert.Equal(t, mem.gets, []string{idA})

	mem.gets = nil

	// A should now be cached
	gets = []store.GetRequest{{Id: idA}}
	cache.GetObjects(ctx, gets)
	assert.NoError(t, gets[0].Err)
	assert.Equal(t, gets[0].Data, []byte(fileA))
	assert.Equal(t, mem.gets, []string(nil))

	idB, err := mem.Store(ctx, []byte(fileB))
	assert.NoError(t, err, "put b")

	gets = []store.GetRequest{{Id: idA}, {Id: idB}}
	cache.GetObjects(ctx, gets)
	assert.Equal(t, gets, []store.GetRequest{
		{Id: idA, Data: []byte(fileA)},
		{Id: idB, Data: []byte(fileB)},
	})
	assert.Equal(t, mem.gets, []string{idB})

	idC, err := mem.Store(ctx, []byte(fileC))
	assert.NoError(t, err, "put c")

	mem.gets = nil
	gets = []store.GetRequest{{Id: idA}, {Id: idC}, {Id: idB}}
	cache.GetObjects(ctx, gets)
	assert.Equal(t, gets, []store.GetRequest{
		{Id: idA, Data: []byte(fileA)},
		{Id: idC, Data: []byte(fileC)},
		{Id: idB, Data: []byte(fileB)},
	})
	assert.Equal(t, mem.gets, []string{idC})
}

func TestEviction(t *testing.T) {
	ctx := context.Background()

	mem := store.InMemory()
	cache := New(mem, t.TempDir(), 1024)

	smallObjs := [][]byte{
		[]byte("a"),
		[]byte("b"),
		[]byte("c"),
	}
	var smallIds []string
	var gets []store.GetRequest
	for _, o := range smallObjs {
		id, err := mem.Store(ctx, o)
		require.NoError(t, err)
		smallIds = append(smallIds, id)
		gets = append(gets, store.GetRequest{Id: id})
	}

	cache.GetObjects(ctx, gets)
	assert.Equal(t, 3, len(cache.objects.have))

	bigObject := make([]byte, 1024-5-len(smallIds[0]))
	rand.Reader.Read(bigObject)

	bigId, err := mem.Store(ctx, bigObject)
	require.NoError(t, err)

	gets = []store.GetRequest{{Id: bigId}}
	cache.GetObjects(ctx, gets)
	assert.Equal(t, 1, len(cache.objects.have))
	assert.NotNil(t, cache.objects.have[bigId])
	log.Printf("bytes=%d", cache.objects.bytes)
	assert.LessOrEqual(t, cache.objects.bytes, uint64(1024))

	tooBig := append(bigObject, bigObject...)
	tooBigId, err := mem.Store(ctx, tooBig)
	require.NoError(t, err)
	gets = []store.GetRequest{{Id: tooBigId}}
	cache.GetObjects(ctx, gets)
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
