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

package storeutil

import "sync"

type entry struct {
	wait chan struct{}
	ok   bool
}

type Cache struct {
	sync.Mutex
	seen map[string]*entry
}

type UploadHandle struct {
	ent      *entry
	resolved bool
}

func (u *UploadHandle) Complete() {
	u.ent.ok = true
	u.resolved = true
	close(u.ent.wait)
}

func (u *UploadHandle) Rollback() {
	if u.resolved {
		return
	}
	u.ent.ok = false
	u.resolved = true
	close(u.ent.wait)
}

func (c *Cache) HasObject(id string) bool {
	c.Lock()
	ent, ok := c.seen[id]
	c.Unlock()
	if !ok {
		return false
	}
	<-ent.wait
	return ent.ok
}

func (c *Cache) StartUpload(id string) UploadHandle {
	c.Lock()
	defer c.Unlock()
	if c.seen == nil {
		c.seen = make(map[string]*entry)
	}
	ent := &entry{wait: make(chan struct{})}
	c.seen[id] = ent
	return UploadHandle{ent: ent}
}

func (c *Cache) RemoveObject(id string) {
	c.Lock()
	defer c.Unlock()
	delete(c.seen, id)
}
