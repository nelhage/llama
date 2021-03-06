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

func TestBoolConfigTrue(t *testing.T) {
	for _, v := range []string{"Y", "y", "9", "1", "foo"} {
		assert.True(t, BoolConfigTrue(v))
	}

	for _, v := range []string{"N", "n", "0", ""} {
		assert.False(t, BoolConfigTrue(v))
	}
}

func TestStringArrayConfig(t *testing.T) {
	assert.Equal(t, []string(nil), StringArrayConfig(""))
	assert.Equal(t, []string{"b", "a"}, StringArrayConfig("b,a"))
	assert.Equal(t, []string{"b", "a"}, StringArrayConfig("b,, ,a"))
	assert.Equal(t, []string{"b", "a"}, StringArrayConfig("b  ,\t a"))
	assert.Equal(t, []string(nil), StringArrayConfig(",,,,"))
}
