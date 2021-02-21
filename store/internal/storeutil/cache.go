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

type Cache struct {
	sync.Mutex
	seen map[string]struct{}
}

func (c *Cache) HasObject(id string) bool {
	c.Lock()
	defer c.Unlock()
	if c.seen == nil {
		return false
	}
	_, ok := c.seen[id]
	return ok
}

func (c *Cache) AddObject(id string) {
	c.Lock()
	defer c.Unlock()
	if c.seen == nil {
		c.seen = make(map[string]struct{})
	}
	c.seen[id] = struct{}{}
}
