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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseMakeDeps(t *testing.T) {
	cases := []struct {
		Src  string
		Deps []string
	}{
		{
			`foo.o: foo.c`,
			[]string{"foo.c"},
		},
		{
			`foo.o: `,
			nil,
		},
		{
			`foo.o: a\ b.c \
 foo.h`,
			[]string{"a b.c", "foo.h"},
		},
		{
			`foo.o: a\b.c foo\\bar.h`,
			[]string{"a\\b.c", "foo\\bar.h"},
		},
	}
	for _, tc := range cases {
		got, err := parseMakeDeps([]byte(tc.Src))
		if err != nil {
			t.Fatalf("parse: %s", err.Error())
		}
		assert.Equal(t, tc.Deps, got)
	}
}
