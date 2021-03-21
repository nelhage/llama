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
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"sync"
)

const debugCache = false

type Cache struct {
	maxBytes uint64
	root     string

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

func New(path string, limit uint64) *Cache {
	st := &Cache{
		maxBytes: limit,
		root:     path,
		objects: objectTracker{
			have: make(map[string]*entry),
		},
	}
	st.objects.head.next = &st.objects.head
	st.objects.head.prev = &st.objects.head
	return st
}

func (st *Cache) Put(key string, obj []byte) {
	st.addToCache(key, obj)
}

func (st *Cache) Get(key string) ([]byte, bool) {
	st.objects.Lock()
	defer st.objects.Unlock()
	if _, ok := st.objects.have[key]; !ok {
		return nil, false
	}
	data, err := st.getOneCached(key)
	if err != nil {
		log.Printf("cache.get(%q): %s", key, err.Error())
	}
	return data, true
}

func (st *Cache) pathFor(id string) string {
	return path.Join(st.root, id[:2], id[2:])
}

func (st *Cache) getOneCached(id string) ([]byte, error) {
	data, err := ioutil.ReadFile(st.pathFor(id))
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (st *Cache) addToCache(id string, data []byte) {
	st.objects.Lock()
	defer st.objects.Unlock()
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
		file := st.pathFor(id)
		os.Mkdir(path.Dir(file), 0755)
		if err := ioutil.WriteFile(file, data, 0644); err != nil {
			log.Printf("Error writing to cache! path=%s err=%q", file, err.Error())
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
		os.Remove(st.pathFor(ent.id))
		st.objects.head.prev = ent.prev
		ent.prev.next = &st.objects.head
		delete(st.objects.have, ent.id)
		st.objects.bytes -= ent.bytes
		st.objects.checkConsistency()
	}
}
