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
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"runtime/trace"
	"strings"

	"github.com/nelhage/llama/protocol"
	"github.com/nelhage/llama/store"
)

// Only one of Path and Bytes should be set. The purpose of this
// abstraction is to allow callers to pass either local files or byte
// slices to get mapped to remote files.
type LocalFile struct {
	Path string

	Bytes []byte
	Mode  os.FileMode
}

type Mapped struct {
	Local  LocalFile
	Remote string
}

type List []Mapped

func (f List) String() string {
	return ""
}

func (f List) Get() interface{} {
	return f
}

func (f *List) Set(v string) error {
	idx := strings.IndexRune(v, ':')
	var source, dest string
	if idx > 0 {
		source = v[:idx]
		dest = v[idx+1:]
	} else {
		source = v
		dest = v
	}
	if path.IsAbs(dest) {
		return fmt.Errorf("-file: cannot expose file at absolute path: %q", dest)
	}
	*f = f.Append(Mapped{Local: LocalFile{Path: source}, Remote: dest})
	return nil
}

func (f List) Append(mapped ...Mapped) List {
	return append(f, mapped...)
}

func (f List) Upload(ctx context.Context, store store.Store, files protocol.FileList) (protocol.FileList, error) {
	var outErr error
	trace.WithRegion(ctx, "uploadFiles", func() {
		for _, file := range f {
			var data []byte
			var mode os.FileMode
			var err error

			if file.Local.Bytes != nil {
				if file.Local.Path != "" {
					panic("MappedFile: got both Path and Bytes")
				}
				data = file.Local.Bytes
				mode = file.Local.Mode
			} else {
				data, err = ioutil.ReadFile(file.Local.Path)
				if err != nil {
					outErr = fmt.Errorf("reading file %q: %w", file.Local.Path, err)
					return
				}
				st, err := os.Stat(file.Local.Path)
				if err != nil {
					outErr = fmt.Errorf("stat %q: %w", file.Local.Path, err)
					return
				}
				mode = st.Mode()
			}
			blob, err := protocol.NewBlob(ctx, store, data)
			if err != nil {
				outErr = err
				return
			}
			files = append(files, protocol.FileAndPath{
				File: protocol.File{Blob: *blob, Mode: mode},
				Path: file.Remote,
			})
		}
	})
	if outErr != nil {
		return nil, outErr
	}
	return files, nil
}

func (f List) Fetch(ctx context.Context, store store.Store, outputs protocol.FileList) error {
	var outErr error

	trace.WithRegion(ctx, "fetchOutputs", func() {
		byPath := make(map[string]*Mapped)
		for i := range f {
			file := &f[i]
			if file.Local.Path == "" || file.Local.Bytes != nil {
				panic("Fetch: local file must be a path")
			}
			byPath[file.Local.Path] = file
		}
		for _, out := range outputs {
			mapped, ok := byPath[out.Path]
			if !ok {
				outErr = fmt.Errorf("Command return unrequested file: %q", out.Path)
				log.Printf(outErr.Error())
				continue
			}
			if err := out.Fetch(ctx, store, mapped.Local.Path); err != nil {
				log.Printf("Fetch %q: %s", mapped.Local.Path, err.Error())
				outErr = err
			}
		}
	})
	return outErr
}
