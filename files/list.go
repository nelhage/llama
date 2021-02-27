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
	"os"
	"path"
	"strings"
	"sync"

	"github.com/nelhage/llama/protocol"
	"github.com/nelhage/llama/protocol/files"
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

func uploadWorker(ctx context.Context, store store.Store, jobs <-chan Mapped, out chan<- *protocol.FileAndPath) {
	for file := range jobs {
		data, mode, err := func() ([]byte, os.FileMode, error) {
			if file.Local.Bytes != nil {
				if file.Local.Path != "" {
					panic("MappedFile: got both Path and Bytes")
				}
				return file.Local.Bytes, file.Local.Mode, nil
			} else {
				data, err := ioutil.ReadFile(file.Local.Path)
				if err != nil {
					return nil, 0, fmt.Errorf("reading file %q: %w", file.Local.Path, err)
				}
				st, err := os.Stat(file.Local.Path)
				if err != nil {
					return nil, 0, fmt.Errorf("stat %q: %w", file.Local.Path, err)
				}
				return data, st.Mode(), nil
			}
		}()
		var blob *protocol.Blob
		if err == nil {
			blob, err = files.NewBlob(ctx, store, data)
		}
		if err != nil {
			blob = &protocol.Blob{Err: err.Error()}
		}
		out <- &protocol.FileAndPath{
			File: protocol.File{Blob: *blob, Mode: mode},
			Path: file.Remote,
		}
	}
}

const uploadConcurrency = 32

func (f List) Upload(ctx context.Context, store store.Store, files protocol.FileList) (protocol.FileList, error) {
	var wg sync.WaitGroup
	jobs := make(chan Mapped)
	out := make(chan *protocol.FileAndPath)

	go func() {
		defer close(jobs)
		for _, file := range f {
			jobs <- file
		}
	}()
	for i := 0; i < uploadConcurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			uploadWorker(ctx, store, jobs, out)
		}()
	}
	go func() {
		wg.Wait()
		close(out)
	}()
	for file := range out {
		files = append(files, *file)
	}

	return files, nil
}

func (f List) TransformToLocal(ctx context.Context, files protocol.FileList) (ok protocol.FileList, bad protocol.FileList) {
	byPath := make(map[string]string)
	for _, out := range f {
		byPath[out.Remote] = out.Local.Path
	}
	for _, out := range files {
		if local, found := byPath[out.Path]; found {
			out.Path = local
			ok = append(ok, out)
		} else {
			bad = append(bad, out)
		}
	}
	return
}

func (f List) MakeAbsolute(base string) List {
	out := make(List, 0, len(f))
	for _, e := range f {
		if e.Local.Path != "" && !path.IsAbs(e.Local.Path) {
			e.Local.Path = path.Join(base, e.Local.Path)
		}
		out = append(out, e)
	}
	return out
}
