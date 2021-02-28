package diskcache

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"sync"

	"github.com/golang/snappy"
	"github.com/nelhage/llama/store"
	"github.com/nelhage/llama/tracing"
)

const debugCache = false

type Store struct {
	maxBytes uint64
	root     string
	inner    store.Store

	objects objectTracker
}

type objectTracker struct {
	sync.Mutex
	bytes uint64
	have  map[string]*entry

	head entry
}

func (o *objectTracker) checkConsistency() {
	if !debugCache {
		return
	}
	var sum uint64
	node := &o.head
	for {
		sum += node.bytes
		if node.next.prev != node {
			panic(fmt.Sprintf("%s.next.prev != self", node.id))
		}
		node = node.next
		if node == &o.head {
			break
		}
	}
	if sum != o.bytes {
		panic(fmt.Sprintf("size mismatch: sum=%d bytes=%d", sum, o.bytes))
	}
}

type entry struct {
	id    string
	bytes uint64
	next  *entry
	prev  *entry
}

func New(inner store.Store, path string, limit uint64) *Store {
	st := &Store{
		maxBytes: limit,
		root:     path,
		inner:    inner,
		objects: objectTracker{
			have: make(map[string]*entry),
		},
	}
	st.objects.head.next = &st.objects.head
	st.objects.head.prev = &st.objects.head
	return st
}

func (st *Store) Store(ctx context.Context, obj []byte) (string, error) {
	return st.inner.Store(ctx, obj)
}

func (st *Store) pathFor(id string) string {
	return path.Join(st.root, id[:2], id[2:])
}

func (st *Store) getOneCached(id string) ([]byte, error) {
	data, err := ioutil.ReadFile(st.pathFor(id))
	if err != nil {
		return nil, err
	}
	uncompressed, err := snappy.Decode(nil, data)
	if err != nil {
		return nil, err
	}
	return uncompressed, nil
}

func (st *Store) addToCache(id string, data []byte) {
	ent, ok := st.objects.have[id]
	if ok {
		// Already in cache; move to the head of the LRU list.
		// Remove from the list, and let the next section
		// re-add it.
		ent.prev.next = ent.next
		ent.next.prev = ent.prev
		st.objects.bytes -= ent.bytes
	} else {
		ent = &entry{
			id:    id,
			bytes: uint64(len(id) + len(data)),
		}
		compressed := snappy.Encode(nil, data)
		file := st.pathFor(id)
		os.Mkdir(path.Dir(file), 0755)
		if err := ioutil.WriteFile(file, compressed, 0644); err != nil {
			log.Printf("Error writing to cach! path=%s err=%q", file, err.Error())
			return
		}
		st.objects.have[id] = ent
	}

	// Add the entry to the head of the list
	head := &st.objects.head
	ent.next = head.next
	ent.prev = head
	head.next.prev = ent
	head.next = ent
	st.objects.bytes += ent.bytes

	st.objects.checkConsistency()

	// Shrink down to fit
	for st.objects.bytes > st.maxBytes {
		// prune the tail object
		ent := st.objects.head.prev
		log.Printf("prune bytes=%d lim=%d next=%q", st.objects.bytes, st.maxBytes, ent.id)
		os.Remove(st.pathFor(ent.id))
		st.objects.head.prev = ent.prev
		ent.prev.next = &st.objects.head
		delete(st.objects.have, ent.id)
		st.objects.bytes -= ent.bytes
		st.objects.checkConsistency()
	}
}

func (st *Store) fillFromCache(gets []store.GetRequest) []store.GetRequest {
	st.objects.Lock()
	defer st.objects.Unlock()
	var todo []store.GetRequest
	for i := range gets {
		if _, ok := st.objects.have[gets[i].Id]; ok {
			gets[i].Data, gets[i].Err = st.getOneCached(gets[i].Id)
		} else {
			gets[i].Data = nil
			gets[i].Err = nil
			todo = append(todo, gets[i])
		}
	}
	return todo
}

func (st *Store) GetObjects(ctx context.Context, gets []store.GetRequest) {
	ctx, span := tracing.StartSpan(ctx, "cached_get")
	defer span.End()

	span.AddField("objects", len(gets))
	todo := st.fillFromCache(gets)
	span.AddField("hits", len(gets)-len(todo))

	st.inner.GetObjects(ctx, todo)
	gi := 0
	for _, got := range todo {
		for gi < len(gets) && (gets[gi].Data != nil || gets[gi].Err != nil) {
			gi += 1
		}
		if gi == len(gets) || gets[gi].Id != got.Id {
			panic(fmt.Sprintf("internal consistency error, gets[%d].Id=%s, todo.Id=%s", gi, gets[gi].Id, got.Id))
		}
		gets[gi] = got
		gi += 1
	}

	st.objects.Lock()
	defer st.objects.Unlock()
	for _, got := range todo {
		if got.Err == nil {
			st.addToCache(got.Id, got.Data)
		}
	}
}
