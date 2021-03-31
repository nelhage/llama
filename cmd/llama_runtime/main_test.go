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

// +build llama.runtime

package main

import (
	"io/ioutil"
	"os"
	"path"
	"reflect"
	"testing"

	"context"

	"github.com/nelhage/llama/protocol"
	"github.com/nelhage/llama/protocol/files"
	"github.com/nelhage/llama/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeCmdline(t *testing.T) {
	tests := []struct {
		handler string
		in      []string
		out     []string
	}{
		{
			"llama-handler",
			[]string{}, []string{"llama-handler"},
		},
		{
			"llama-handler",
			[]string{"hi?"}, []string{"llama-handler"},
		},
		{
			"", []string{}, []string{},
		},
		{
			"",
			[]string{"sh", "/"}, []string{"sh", "/"},
		},
		{
			"",
			[]string{"/bin/sh", "-c", "echo"},
			[]string{"/bin/sh", "-c", `echo "$@"`, "echo"},
		},
		{
			"",
			[]string{"/bin/sh", "-c", "echo", "echo"},
			[]string{"/bin/sh", "-c", "echo", "echo"},
		},
	}

	for _, tc := range tests {
		os.Setenv("_HANDLER", tc.handler)
		got := computeCmdline(tc.in)
		assert.Equal(t, tc.out, got, "_HANDLER=%s computeCmdline(%q)", tc.handler, tc.in)
	}
}

func TestParseJob(t *testing.T) {
	const (
		contentsA = "Hello, A\n"
		contentsB = "This is B\n"
	)

	ctx := context.Background()
	st := store.InMemory()
	a_txt, _ := files.NewBlob(ctx, st, []byte(contentsA))
	b_txt, _ := files.NewBlob(ctx, st, []byte(contentsB))

	cmdline := []string{"/bin/echo", "Hello"}
	spec := protocol.InvocationSpec{
		Args: []string{"World"},
		Files: protocol.FileList{
			{Path: "a.txt", File: protocol.File{Blob: *a_txt}},
			{Path: "indir/b.txt", File: protocol.File{Blob: *b_txt}},
		},
		Outputs: []string{"outdir/c.txt"},
	}

	r := Runtime{store: st, cmdline: cmdline}

	job, err := r.parseJob(ctx, &spec)
	if err != nil {
		t.Fatal("parseJob", err)
	}
	defer job.Cleanup()
	if !reflect.DeepEqual(job.Args, []string{"/bin/echo", "Hello", "World"}) {
		t.Errorf("Bad args: %q", job.Args)
	}
	data, err := ioutil.ReadFile(path.Join(job.Root, "a.txt"))
	if err != nil || string(data) != contentsA {
		t.Errorf("Bad a.txt: %q/%v", data, err)
	}
	data, err = ioutil.ReadFile(path.Join(job.Root, "indir/b.txt"))
	if err != nil || string(data) != contentsB {
		t.Errorf("Bad b.txt: %q/%v", data, err)
	}
	fi, err := os.Stat(path.Join(job.Root, "outdir"))
	if err != nil {
		t.Errorf("coult not stat outdir: %s", err.Error())
	} else if !fi.Mode().IsDir() {
		t.Errorf("outdir should be a directory, is: %d", fi.Mode())
	}
}

func TestRunOne(t *testing.T) {
	const (
		contentsA = "Hello, A\n"
	)

	ctx := context.Background()
	st := store.InMemory()
	a_txt, _ := files.NewBlob(ctx, st, []byte(contentsA))

	cmdline := []string{"/bin/sh", "-c"}
	spec := protocol.InvocationSpec{
		Args: []string{`cat in/a.txt > b.txt; echo World >> b.txt; echo OutPUT; echo STDeRR >&2`},
		Files: protocol.FileList{
			{Path: "in/a.txt", File: protocol.File{Blob: *a_txt}},
		},
		Outputs: []string{"b.txt", "c.txt"},
	}

	r := Runtime{store: st, cmdline: cmdline}
	resp, err := r.RunOne(ctx, &spec)
	if err != nil {
		t.Fatal("runOne", err)
	}

	// c.txt is not created and will not be included in the
	// outputs
	assert.Equal(t, 1, len(resp.Outputs))

	b_blob := resp.Outputs[0]
	assert.Equal(t, "b.txt", b_blob.Path)
	b_txt, err := files.Read(ctx, st, &b_blob.Blob)
	assert.NoError(t, err)
	assert.Equal(t, contentsA+"World\n", string(b_txt))
}

func TestRunOne_NoCmdLine(t *testing.T) {
	ctx := context.Background()
	st := store.InMemory()

	spec := protocol.InvocationSpec{
		Args:    []string{`echo`, `hello`},
		Files:   nil,
		Outputs: nil,
	}

	r := Runtime{store: st}
	resp, err := r.RunOne(ctx, &spec)
	if err != nil {
		t.Fatal("runOne", err)
	}

	stdout, err := files.Read(ctx, st, resp.Stdout)
	require.NoError(t, err)
	assert.Equal(t, stdout, []byte("hello\n"))
}
