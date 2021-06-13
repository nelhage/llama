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
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCompile(t *testing.T) {
	tests := []struct {
		env  []string
		argv []string
		out  Compilation
		err  bool
	}{
		{
			[]string{},
			[]string{
				"cc", "-MD", "-Wall", "-Werror", "-D_GNU_SOURCE", "-g", "-c", "-o", "platform/linux/linux_ptrace.o", "platform/linux/linux_ptrace.c",
			},
			Compilation{
				Language:             "c",
				PreprocessedLanguage: "cpp-output",
				Input:                "platform/linux/linux_ptrace.c",
				Output:               "platform/linux/linux_ptrace.o",
				UnknownArgs:          []string{"-Wall", "-Werror", "-g"},
				LocalArgs:            []string{"-MD", "-Wall", "-Werror", "-D_GNU_SOURCE", "-g", "-MF", "platform/linux/linux_ptrace.d"},
				RemoteArgs:           []string{"-Wall", "-Werror", "-g", "-c"},
				Flag: Flags{
					MD: true,
					C:  true,
					MF: "platform/linux/linux_ptrace.d",
				},
			},
			false,
		},
		{
			[]string{
				"LLAMACC_FILTER_WARNINGS=error,all",
			},
			[]string{
				"cc", "-MD", "-Wall", "-Werror", "-Wno-error", "-D_GNU_SOURCE", "-g", "-c", "-o", "platform/linux/linux_ptrace.o", "platform/linux/linux_ptrace.c",
			}, Compilation{
				Language:             "c",
				PreprocessedLanguage: "cpp-output",
				Input:                "platform/linux/linux_ptrace.c",
				Output:               "platform/linux/linux_ptrace.o",
				UnknownArgs:          []string{"-g"},
				LocalArgs:            []string{"-MD", "-D_GNU_SOURCE", "-g", "-MF", "platform/linux/linux_ptrace.d"},
				RemoteArgs:           []string{"-g", "-c"},
				Flag: Flags{
					MD: true,
					C:  true,
					MF: "platform/linux/linux_ptrace.d",
				},
			},
			false,
		},
		{
			[]string{},
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
			[]string{},
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
			[]string{},
			[]string{
				"/usr/bin/cc", "-DBORINGSSL_DISPATCH_TEST", "-DBORINGSSL_HAVE_LIBUNWIND", "-DBORINGSSL_IMPLEMENTATION", "-I/home/nelhage/code/boringssl/third_party/googletest/include", "-I/home/nelhage/code/boringssl/crypto/../include", "-Wa,--noexecstack", "-Wa,-g", "-o", "CMakeFiles/crypto.dir/chacha/chacha-x86_64.S.o", "-c", "/home/nelhage/code/boringssl/build/crypto/chacha/chacha-x86_64.S",
			},
			Compilation{
				Language:             LangAssemblerWithCpp,
				PreprocessedLanguage: "assembler",
				Input:                "/home/nelhage/code/boringssl/build/crypto/chacha/chacha-x86_64.S",
				Output:               "CMakeFiles/crypto.dir/chacha/chacha-x86_64.S.o",
				UnknownArgs:          []string{"-Wa,--noexecstack", "-Wa,-g"},
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
			cfg := ParseConfig(tc.env)
			got, err := ParseCompile(&cfg, tc.argv)
			// Don't compare includes or defines for now
			got.Includes = nil
			got.Defs = nil
			if tc.err {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, &tc.out, &got)
		})
	}
}

func TestRewriteWp(t *testing.T) {
	cases := []struct {
		in  []string
		out []string
	}{
		{
			[]string{"-Wall"},
			[]string{"-Wall"},
		},
		{
			[]string{"-Wp,-MD,foo.d"},
			[]string{"-MD", "-MF", "foo.d"},
		},
		{
			[]string{"-Wp,-MD,foo.d,-g"},
			[]string{"-MD", "-MF", "foo.d", "-g"},
		},
	}
	for _, tc := range cases {
		got := rewriteWp(tc.in)
		assert.Equal(t, tc.out, got)
	}
}
