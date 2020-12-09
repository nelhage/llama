package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCompile(t *testing.T) {
	tests := []struct {
		argv []string
		out  Compilation
		err  bool
	}{
		{
			[]string{
				"cc", "-MD", "-Wall", "-Werror", "-D_GNU_SOURCE", "-g", "-c", "-o", "platform/linux/linux_ptrace.o", "platform/linux/linux_ptrace.c",
			},
			Compilation{
				Language:   "c",
				Input:      "platform/linux/linux_ptrace.c",
				Output:     "platform/linux/linux_ptrace.o",
				LocalArgs:  []string{"-MD", "-Wall", "-Werror", "-D_GNU_SOURCE", "-g", "-MF", "platform/linux/linux_ptrace.d"},
				RemoteArgs: []string{"-Wall", "-Werror", "-g", "-c"},
				Flag: Flags{
					MD: true,
					C:  true,
				},
			},
			false,
		},
		{
			[]string{
				"cc", "-c", "hello.c",
			},
			Compilation{
				Language:   "c",
				Input:      "hello.c",
				Output:     "hello.o",
				RemoteArgs: []string{"-c"},
				Flag: Flags{
					C: true,
				},
			},
			false,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(strings.Join(tc.argv, " "), func(t *testing.T) {
			t.Parallel()
			got, err := ParseCompile(tc.argv)
			if tc.err {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, &tc.out, &got)
		})
	}
}
