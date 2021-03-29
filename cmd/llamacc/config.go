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
	"log"
	"strings"
)

type Config struct {
	Verbose         bool
	Local           bool
	RemoteAssemble  bool
	FullPreprocess  bool
	Function        string
	LocalPreprocess bool
	BuildID         string

	LocalCC  string
	LocalCXX string
}

var DefaultConfig = Config{
	Function: "gcc",
	LocalCC:  "cc",
	LocalCXX: "c++",
}

func ParseConfig(env []string) Config {
	out := DefaultConfig
	for _, ev := range env {
		if !strings.HasPrefix(ev, "LLAMACC_") {
			continue
		}
		var eq = strings.IndexRune(ev, '=')
		if eq < 0 {
			panic("env var missing `=`?")
		}
		key := ev[len("LLAMACC_"):eq]
		val := ev[eq+1:]
		switch key {
		case "VERBOSE":
			out.Verbose = val != ""
		case "LOCAL":
			out.Local = val != ""
		case "REMOTE_ASSEMBLE":
			out.RemoteAssemble = val != ""
		case "FUNCTION":
			out.Function = val
		case "FULL_PREPROCESS":
			out.FullPreprocess = val != ""
		case "LOCAL_PREPROCESS":
			out.LocalPreprocess = val != ""
		case "BUILD_ID":
			out.BuildID = val
		case "LOCAL_CC":
			out.LocalCC = val
		case "LOCAL_CXX":
			out.LocalCXX = val
		default:
			log.Printf("llamacc: unknown env var: %s", ev)
		}
	}
	return out
}
