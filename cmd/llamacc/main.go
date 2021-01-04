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
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"strings"
)

func runLlamaCC(cfg *Config, comp *Compilation) error {
	var err error
	var preprocessor exec.Cmd
	ccpath, err := exec.LookPath(comp.Compiler())
	if err != nil {
		return err
	}
	preprocessor.Path = ccpath
	preprocessor.Args = []string{comp.Compiler()}
	preprocessor.Args = append(preprocessor.Args, comp.LocalArgs...)
	preprocessor.Args = append(preprocessor.Args, "-fdirectives-only")
	preprocessor.Args = append(preprocessor.Args, "-E", "-o", "-", comp.Input)
	var preprocessed bytes.Buffer
	preprocessor.Stdout = &preprocessed
	preprocessor.Stderr = os.Stderr
	if cfg.Verbose {
		log.Printf("run cpp: %q", preprocessor.Args)
	}
	if err := preprocessor.Run(); err != nil {
		return err
	}

	var compiler exec.Cmd
	if cfg.Local {
		compiler.Path = ccpath
		compiler.Args = []string{comp.Compiler()}
		compiler.Args = append(compiler.Args, comp.RemoteArgs...)
		compiler.Args = append(compiler.Args, "-fdirectives-only", "-fpreprocessed")
		compiler.Args = append(compiler.Args, "-x", comp.PreprocessedLanguage, "-o", comp.Output, "-")
		compiler.Stderr = os.Stderr
		compiler.Stdin = &preprocessed
	} else {
		var llama string
		if strings.IndexRune(os.Args[0], '/') >= 0 {
			llama = path.Join(path.Dir(os.Args[0]), "llama")
		} else {
			llama, err = exec.LookPath("llama")
			if err != nil {
				return fmt.Errorf("can't find llama executable: %s", err.Error())
			}
		}
		compiler.Path = llama
		compiler.Args = []string{"llama", "invoke", "-o", comp.Output, "-stdin", cfg.Function, comp.Compiler()}
		compiler.Args = append(compiler.Args, comp.RemoteArgs...)
		compiler.Args = append(compiler.Args, "-fdirectives-only", "-fpreprocessed")
		compiler.Args = append(compiler.Args, "-x", comp.PreprocessedLanguage, "-o", comp.Output, "-")
		compiler.Stderr = os.Stderr
		compiler.Stdin = &preprocessed
	}

	if cfg.Verbose {
		log.Printf("run %s: %q", comp.Compiler(), compiler.Args)
	}
	if err := compiler.Run(); err != nil {
		return err
	}

	return nil
}

func checkSupported(cfg *Config, comp *Compilation) error {
	if (comp.Language == LangAssembler || comp.Language == LangAssemblerWithCpp) &&
		!cfg.RemoteAssemble {
		return errors.New("Assembly requested, and LLAMACC_REMOTE_ASSEMBLE unset")
	}
	return nil
}

func main() {
	cfg := ParseConfig(os.Environ())
	comp, err := ParseCompile(&cfg, os.Args)
	if err == nil {
		err = checkSupported(&cfg, &comp)
	}
	if err == nil {
		err = runLlamaCC(&cfg, &comp)
		if err != nil {
			if ex, ok := err.(*exec.ExitError); ok {
				os.Exit(ex.ExitCode())
			}
			fmt.Fprintf(os.Stderr, "Running gcc: %s\n", err.Error())
			os.Exit(1)
		}
		os.Exit(0)
	}
	if cfg.Verbose {
		log.Printf("[llamacc] compiling locally: %s (%q)", err.Error(), os.Args)
	}

	cc := "gcc"
	if strings.HasSuffix(os.Args[0], "cxx") || strings.HasSuffix(os.Args[0], "c++") {
		cc = "g++"
	}

	cmd := exec.Command(cc, os.Args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	if err := cmd.Run(); err != nil {
		if ex, ok := err.(*exec.ExitError); ok {
			os.Exit(ex.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "Running gcc: %s\n", err.Error())
		os.Exit(1)
	}
}
