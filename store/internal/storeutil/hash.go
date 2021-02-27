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

import (
	"encoding/hex"

	"golang.org/x/crypto/blake2b"
)

func HashObject(obj []byte) string {
	csum := blake2b.Sum256(obj)
	id := hex.EncodeToString(csum[:])
	return id
}
