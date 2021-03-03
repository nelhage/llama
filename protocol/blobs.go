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
	"os"
)

const MaxInlineBlob = 100

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

type FileAndPath struct {
	File
	Path string `json:"p"`
}

type FileList []FileAndPath
