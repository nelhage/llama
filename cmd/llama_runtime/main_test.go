package main

import (
	"os"
	"reflect"
	"testing"
)

func TestComputeCmdline(t *testing.T) {
	os.Setenv("_HANDLER", "llama-handler")

	tests := []struct {
		in  []string
		out []string
	}{
		{
			[]string{}, []string{"llama-handler"},
		},
		{
			[]string{"sh", "/"}, []string{"sh", "/"},
		},
		{
			[]string{"/bin/sh", "-c", "echo"},
			[]string{"/bin/sh", "-c", `echo "$@"`, "echo"},
		},
		{
			[]string{"/bin/sh", "-c", "echo", "echo"},
			[]string{"/bin/sh", "-c", "echo", "echo"},
		},
	}

	for _, tc := range tests {
		got := computeCmdline(tc.in)
		if !reflect.DeepEqual(got, tc.out) {
			t.Errorf("computeCmdline(%q): got %q != %q", tc.in, got, tc.out)
		}
	}
}
