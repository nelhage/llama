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
	"errors"
	"fmt"
	"path"
	"strings"
)

type Lang string

const (
	LangC                Lang = "c"
	LangCxx              Lang = "c++"
	LangAssembler        Lang = "assembler"
	LangAssemblerWithCpp Lang = "assembler-with-cpp"
)

var knownLangs = map[string]Lang{
	string(LangC):                LangC,
	string(LangCxx):              LangCxx,
	string(LangAssembler):        LangAssembler,
	string(LangAssemblerWithCpp): LangAssemblerWithCpp,
}

var extLangs = map[string]Lang{
	".c":   LangC,
	".cxx": LangCxx,
	".cc":  LangCxx,
	".cpp": LangCxx,
	".s":   LangAssembler,
	".S":   LangAssemblerWithCpp,
}

var preprocessedLang = map[Lang]string{
	LangCxx:              "c++-cpp-output",
	LangC:                "cpp-output",
	LangAssemblerWithCpp: "assembler",
}

type Compilation struct {
	Language             Lang
	PreprocessedLanguage string
	Input                string
	Output               string
	LocalArgs            []string
	RemoteArgs           []string
	Flag                 Flags
}

func (c *Compilation) Compiler() string {
	if c.Language == "c++" {
		return "g++"
	}
	return "gcc"
}

type Flags struct {
	MD bool
	C  bool
	S  bool
	MF string
}

func smellsLikeInput(arg string) bool {
	ext := path.Ext(arg)
	_, ok := extLangs[ext]
	return ok

	/*
		if fi, err := os.Stat(arg); err != nil || fi.IsDir() {
			return false
		}
		// We were passed a filename that exists. Maybe check for
		// extension?
		return true
	*/
}

type argAction struct {
	filterLocal  bool
	filterRemote bool
	err          error
}

func eatArg(argv []string, flag string) (string, bool) {
	if argv[0] == flag {
		if len(argv) == 1 {
			return "", true
		}
		return argv[1], true
	}
	return argv[0][len(flag):], false
}

type argSpec struct {
	flag   string
	action func(c *Compilation, arg string) argAction
	hasArg bool
}

var argSpecs = []argSpec{
	{"-MD", func(c *Compilation, _ string) argAction {
		c.Flag.MD = true
		return argAction{filterRemote: true}
	}, false},
	{"-MF", func(c *Compilation, arg string) argAction {
		c.Flag.MF = arg
		return argAction{filterRemote: true}
	}, true},
	{"-D", func(c *Compilation, arg string) argAction {
		return argAction{filterRemote: true}
	}, true},
	{"-U", func(c *Compilation, arg string) argAction {
		return argAction{filterRemote: true}
	}, true},
	{"-c", func(c *Compilation, arg string) argAction {
		c.Flag.C = true
		return argAction{filterLocal: true}
	}, false},
	{"-E", func(c *Compilation, arg string) argAction {
		return argAction{err: errors.New("-E given")}
	}, false},
	{"-S", func(c *Compilation, arg string) argAction {
		c.Flag.S = true
		return argAction{err: errors.New("-S given")}
	}, false},
	{"-x", func(c *Compilation, arg string) argAction {
		lang, ok := knownLangs[arg]
		if ok {
			c.Language = lang
		} else {
			return argAction{err: fmt.Errorf("Unsupported language: %s", arg)}
		}
		return argAction{filterRemote: true}
	}, true},
	{"-o", func(c *Compilation, arg string) argAction {
		if c.Output != "" {
			return argAction{err: fmt.Errorf("multiple outputs: %s, %s", c.Output, arg)}
		}
		c.Output = arg
		return argAction{filterRemote: true, filterLocal: true}
	}, true},
	{"-I", func(c *Compilation, _ string) argAction {
		return argAction{filterRemote: true}
	}, true},
	{"-isystem", func(c *Compilation, _ string) argAction {
		return argAction{filterRemote: true}
	}, true},
	{"-iquote", func(c *Compilation, _ string) argAction {
		return argAction{filterRemote: true}
	}, true},
	{"-idirafter", func(c *Compilation, _ string) argAction {
		return argAction{filterRemote: true}
	}, true},
	{"-iprefix", func(c *Compilation, _ string) argAction {
		return argAction{filterRemote: true}
	}, true},
	{"-iwithprefixbefore", func(c *Compilation, _ string) argAction {
		return argAction{filterRemote: true}
	}, true},
	{"-iwithprefix", func(c *Compilation, _ string) argAction {
		return argAction{filterRemote: true}
	}, true},
	{"-isysroot", func(c *Compilation, _ string) argAction {
		return argAction{filterRemote: true}
	}, true},
	{"-nostdinc", func(c *Compilation, _ string) argAction {
		return argAction{filterRemote: true}
	}, true},
}

func replaceExt(file string, newExt string) string {
	if newExt[0] != '.' {
		panic("replaceExt: provided extension must start with `.`")
	}
	ext := path.Ext(file)
	return file[:len(file)-len(ext)] + newExt
}

func ParseCompile(argv []string) (Compilation, error) {
	var out Compilation
	args := argv[1:]

	i := 0
	for i < len(args) {
		arg := args[i]
		i++
		if strings.HasPrefix(arg, "-") {
			found := false
			for _, spec := range argSpecs {
				if !strings.HasPrefix(arg, spec.flag) {
					continue
				}
				var flagArg string
				var eat bool
				if spec.hasArg {
					flagArg, eat = eatArg(args[i-1:], spec.flag)
					if eat {
						i++
						if i > len(args) {
							return out, fmt.Errorf("%s: expected arg", spec.flag)
						}
					}
				}
				act := spec.action(&out, flagArg)
				if act.err != nil {
					return out, act.err
				}
				if !act.filterLocal {
					out.LocalArgs = append(out.LocalArgs, arg)
					if eat {
						out.LocalArgs = append(out.LocalArgs, flagArg)
					}
				}
				if !act.filterRemote {
					out.RemoteArgs = append(out.RemoteArgs, arg)
					if eat {
						out.RemoteArgs = append(out.RemoteArgs, flagArg)
					}
				}
				found = true
				break
			}
			if !found {
				out.LocalArgs = append(out.LocalArgs, arg)
				out.RemoteArgs = append(out.RemoteArgs, arg)
			}
		} else if smellsLikeInput(arg) {
			if out.Input != "" {
				return out, fmt.Errorf("multiple inputs given: %s, %s", out.Input, arg)
			}
			out.Input = arg
		} else {
			out.LocalArgs = append(out.LocalArgs, arg)
			out.RemoteArgs = append(out.RemoteArgs, arg)
		}
	}

	if out.Input == "" {
		return out, errors.New("no supported input detected")
	}
	if !out.Flag.C {
		return out, errors.New("-c not detected")
	}
	if out.Output == "" {
		out.Output = replaceExt(out.Input, ".o")
	}
	if out.Flag.MD && out.Flag.MF == "" {
		out.LocalArgs = append(out.LocalArgs, "-MF", replaceExt(out.Output, ".d"))
	}
	if out.Language == "" {
		lang, ok := extLangs[path.Ext(out.Input)]
		if !ok {
			return out, fmt.Errorf("Unsupported extension: %s", out.Input)
		}
		out.Language = lang
	}
	out.PreprocessedLanguage = preprocessedLang[out.Language]
	if out.PreprocessedLanguage == "" {
		return out, fmt.Errorf("Don't know what happens when we preprocess %s", out.Language)
	}

	return out, nil
}
