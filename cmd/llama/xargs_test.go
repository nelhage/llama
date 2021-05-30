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

package main

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"reflect"
	"strings"
	"testing"

	fs "github.com/nelhage/llama/files"
	"github.com/nelhage/llama/protocol"
	"github.com/nelhage/llama/protocol/files"
	"github.com/nelhage/llama/store"
	"github.com/stretchr/testify/assert"
)

func must(t *testing.T, e error) {
	if e != nil {
		t.Fatal(e)
	}
}

type expectation struct {
	Args    []string
	Files   map[string][]byte
	Outputs []string
}

func readFiles(t *testing.T, ctx context.Context, st store.Store, fl protocol.FileList) map[string][]byte {
	gotFiles := make(map[string][]byte)
	for _, f := range fl {
		data, err := files.Read(ctx, st, &f.Blob)
		must(t, err)
		gotFiles[f.Path] = data
	}
	return gotFiles
}

func assertSpec(t *testing.T, ctx context.Context, st store.Store, name string, want *expectation, got *protocol.InvocationSpec) {
	assert.Equal(t, want.Args, got.Args, "%s: args", name)
	assert.Equal(t, want.Outputs, got.Outputs, "%s: outputs", name)
	gotFiles := readFiles(t, ctx, st, got.Files)
	assert.Equal(t, want.Files, gotFiles, "%s: files", name)
}

func generateAndPrepare(
	t *testing.T, ctx context.Context, st store.Store,
	files protocol.FileList,
	input string, args []string) []*protocol.InvocationSpec {
	read := strings.NewReader(input)
	jobs := make(chan *Invocation)
	go generateJobs(context.Background(), read, args, jobs)
	var specs []*protocol.InvocationSpec
	for job := range jobs {
		spec, err := prepareInvocation(ctx, st, files, job)
		if err != nil {
			t.Fatalf("prepare: %s", err.Error())
		}
		specs = append(specs, spec)
	}
	return specs
}

func TestPrepareInvocation_Xargs(t *testing.T) {
	ctx := context.Background()
	st := store.InMemory()

	var input bytes.Buffer
	fmt.Fprintln(&input, "a")
	fmt.Fprintln(&input, "b")
	tmp := t.TempDir()
	const (
		contentsA      = "this is A.txt"
		contentsB      = "but this is entirely different\n"
		contentsCommon = "and this is entirely other\n"
	)

	must(t, ioutil.WriteFile(path.Join(tmp, "a.txt"), []byte(contentsA), 0644))
	must(t, ioutil.WriteFile(path.Join(tmp, "b.txt"), []byte(contentsB), 0644))
	must(t, ioutil.WriteFile(path.Join(tmp, "common.txt"), []byte(contentsCommon), 0644))

	oldpwd, _ := fs.WorkingDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %s", err.Error())
	}
	defer os.Chdir(oldpwd)
	specs := generateAndPrepare(t, ctx, st, nil,
		input.String(), []string{
			"-n{{.Idx}}",
			`{{.I (printf "%s.txt" .Line)}}`,
			`{{.O (printf "out/%s.txt" .Line)}}`,
			`{{.IO "common.txt"}}`,
		})

	want := []expectation{
		{
			Args: []string{"-n0", "a.txt", "out/a.txt", "common.txt"},
			Files: map[string][]byte{
				"a.txt":      []byte(contentsA),
				"common.txt": []byte(contentsCommon),
			},
			Outputs: []string{"out/a.txt", "common.txt"},
		},
		{
			Args: []string{"-n1", "b.txt", "out/b.txt", "common.txt"},
			Files: map[string][]byte{
				"b.txt":      []byte(contentsB),
				"common.txt": []byte(contentsCommon),
			},
			Outputs: []string{"out/b.txt", "common.txt"},
		},
	}

	assert.Equal(t, len(want), len(specs))

	for i, w := range want {
		got := specs[i]
		assertSpec(t, ctx, st, fmt.Sprintf("spec %d", i), &w, got)
	}
}

func TestPrepareInvocation_Files(t *testing.T) {
	ctx := context.Background()
	st := store.InMemory()

	const (
		fileContents = "file 1\n"
	)

	blob, err := files.NewBlob(ctx, st, []byte(fileContents))
	must(t, err)
	files := protocol.FileList{
		{Path: "file.txt", File: protocol.File{Blob: *blob, Mode: 0644}},
	}

	specs := generateAndPrepare(t, ctx, st, files, "line\n", []string{"echo"})

	if reflect.ValueOf(specs[0].Files).Pointer() != reflect.ValueOf(files).Pointer() {
		t.Errorf("with no file inputs, should preserve provided files got=%#v", specs[0].Files)
	}

	specs = generateAndPrepare(t, ctx, st, files, "line\n", []string{"echo", `{{.O "out.txt"}}`})

	if reflect.ValueOf(specs[0].Files).Pointer() != reflect.ValueOf(files).Pointer() {
		t.Errorf("with only outputs, should preserve provided files got=%#v", specs[0].Files)
	}

	specs = generateAndPrepare(t, ctx, st, files, "line\n", []string{"echo", `{{.AsFile "hello"}}`})

	if reflect.ValueOf(specs[0].Files).Pointer() == reflect.ValueOf(files).Pointer() {
		t.Errorf("with a file input, should modify file")
	}
	gotFiles := readFiles(t, ctx, st, specs[0].Files)
	wantFiles := map[string][]byte{
		"file.txt":       []byte(fileContents),
		specs[0].Args[1]: []byte("hello\n"),
	}
	assert.Equal(t, wantFiles, gotFiles, "one AsFile")

	specs = generateAndPrepare(t, ctx, st, files, "line\n", []string{"echo", `{{.I "testdata/a.txt"}}`})

	if reflect.ValueOf(specs[0].Files).Pointer() == reflect.ValueOf(files).Pointer() {
		t.Errorf("with a .Input, should modify file")
	}
	a_txt, err := ioutil.ReadFile(`testdata/a.txt`)
	assert.NoError(t, err)
	wantFiles = map[string][]byte{
		"file.txt":       []byte(fileContents),
		"testdata/a.txt": a_txt,
	}
	gotFiles = readFiles(t, ctx, st, specs[0].Files)
	assert.Equal(t, wantFiles, gotFiles, "one .I")

	specs = generateAndPrepare(t, ctx, st, files, "line\n",
		[]string{"echo", `{{.I "testdata/a.txt"}}`, `{{.I "testdata/b.txt"}}`, `{{.AsFile "llamas"}}`})

	if reflect.ValueOf(specs[0].Files).Pointer() == reflect.ValueOf(files).Pointer() {
		t.Errorf("with a .Input, should modify file")
	}
	b_txt, err := ioutil.ReadFile(`testdata/b.txt`)
	assert.NoError(t, err)
	wantFiles = map[string][]byte{
		"file.txt":       []byte(fileContents),
		"testdata/a.txt": a_txt,
		"testdata/b.txt": b_txt,
		specs[0].Args[3]: []byte("llamas\n"),
	}
	gotFiles = readFiles(t, ctx, st, specs[0].Files)
	assert.Equal(t, wantFiles, gotFiles, ".I and .AsFile")
}
