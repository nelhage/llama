package main

import (
	"fmt"
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
				Language:             "c",
				PreprocessedLanguage: "cpp-output",
				Input:                "platform/linux/linux_ptrace.c",
				Output:               "platform/linux/linux_ptrace.o",
				LocalArgs:            []string{"-MD", "-Wall", "-Werror", "-D_GNU_SOURCE", "-g", "-MF", "platform/linux/linux_ptrace.d"},
				RemoteArgs:           []string{"-Wall", "-Werror", "-g", "-c"},
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
				Language:             "c",
				PreprocessedLanguage: "cpp-output",
				Input:                "hello.c",
				Output:               "hello.o",
				RemoteArgs:           []string{"-c"},
				Flag: Flags{
					C: true,
				},
			},
			false,
		},
		{
			[]string{
				"cc", "-c", "hello.c", "-o", "hello.o",
			},
			Compilation{
				Language:             "c",
				PreprocessedLanguage: "cpp-output",
				Input:                "hello.c",
				Output:               "hello.o",
				RemoteArgs:           []string{"-c"},
				Flag: Flags{
					C: true,
				},
			},
			false,
		},
		{
			[]string{
				"/usr/bin/cc", "-DBORINGSSL_DISPATCH_TEST", "-DBORINGSSL_HAVE_LIBUNWIND", "-DBORINGSSL_IMPLEMENTATION", "-I/home/nelhage/code/boringssl/third_party/googletest/include", "-I/home/nelhage/code/boringssl/crypto/../include", "-Wa,--noexecstack", "-Wa,-g", "-o", "CMakeFiles/crypto.dir/chacha/chacha-x86_64.S.o", "-c", "/home/nelhage/code/boringssl/build/crypto/chacha/chacha-x86_64.S",
			},
			Compilation{
				Language:             LangAssemblerWithCpp,
				PreprocessedLanguage: "assembler",
				Input:                "/home/nelhage/code/boringssl/build/crypto/chacha/chacha-x86_64.S",
				Output:               "CMakeFiles/crypto.dir/chacha/chacha-x86_64.S.o",
				LocalArgs:            []string{"-DBORINGSSL_DISPATCH_TEST", "-DBORINGSSL_HAVE_LIBUNWIND", "-DBORINGSSL_IMPLEMENTATION", "-I/home/nelhage/code/boringssl/third_party/googletest/include", "-I/home/nelhage/code/boringssl/crypto/../include", "-Wa,--noexecstack", "-Wa,-g"},
				RemoteArgs:           []string{"-Wa,--noexecstack", "-Wa,-g", "-c"},
				Flag: Flags{
					C: true,
				},
			},
			false,
		},
	}
	for i, tc := range tests {
		tc := tc
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
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
