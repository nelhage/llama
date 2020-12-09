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
	a_txt, _ := protocol.NewBlob(ctx, st, []byte(contentsA))
	b_txt, _ := protocol.NewBlob(ctx, st, []byte(contentsB))

	cmdline := []string{"/bin/echo", "Hello"}
	spec := protocol.InvocationSpec{
		Args: []string{"World"},
		Files: map[string]protocol.File{
			"a.txt":       protocol.File{Blob: *a_txt},
			"indir/b.txt": protocol.File{Blob: *b_txt},
		},
		Outputs: []string{"outdir/c.txt"},
	}

	job, err := parseJob(ctx, st, cmdline, &spec)
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
	a_txt, _ := protocol.NewBlob(ctx, st, []byte(contentsA))

	cmdline := []string{"/bin/sh", "-c"}
	spec := protocol.InvocationSpec{
		Args: []string{`cat in/a.txt > b.txt; echo World >> b.txt; echo OutPUT; echo STDeRR >&2`},
		Files: map[string]protocol.File{
			"in/a.txt": protocol.File{Blob: *a_txt},
		},
		Outputs: []string{"b.txt", "c.txt"},
	}

	resp, err := runOne(ctx, st, cmdline, &spec)
	if err != nil {
		t.Fatal("runOne", err)
	}

	b_blob := resp.Outputs["b.txt"]
	b_txt, err := b_blob.Read(ctx, st)
	if err != nil {
		t.Errorf("Read b.txt: %s", err.Error())
	} else if string(b_txt) != contentsA+"World\n" {
		t.Errorf("Read b.txt: wrong contents %s", b_txt)
	}

	if c := resp.Outputs["c.txt"]; c.Err == "" {
		t.Errorf("reading c: expected error, got %#v", c)
	}
}

func TestRunOne_NoCmdLine(t *testing.T) {
	ctx := context.Background()
	st := store.InMemory()

	spec := protocol.InvocationSpec{
		Args:    []string{`echo`, `hello`},
		Files:   nil,
		Outputs: nil,
	}

	resp, err := runOne(ctx, st, nil, &spec)
	if err != nil {
		t.Fatal("runOne", err)
	}

	stdout, err := resp.Stdout.Read(ctx, st)
	require.NoError(t, err)
	assert.Equal(t, stdout, []byte("hello\n"))
}
