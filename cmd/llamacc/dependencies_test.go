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
