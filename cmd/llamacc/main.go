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
	"log"
	"os"
	"os/exec"
	"path"
	"strings"
)

func toRemote(local, wd string) string {
	var remote string
	if local[0] == '/' {
		remote = local[1:]
	} else {
		remote = path.Join(wd, local)[1:]
	}
	return path.Join("_root", remote)
}

func remap(local, wd string) string {
	return fmt.Sprintf("%s:%s", local, toRemote(local, wd))
}

func runLlamaCC(cfg *Config, comp *Compilation) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	deps, err := detectDependencies(cfg, comp)
	if err != nil {
		return fmt.Errorf("Detecting dependencies: %w", err)
	}

	var cmd exec.Cmd

	var llama string
	if strings.IndexRune(os.Args[0], '/') >= 0 {
		llama = path.Join(path.Dir(os.Args[0]), "llama")
	} else {
		llama, err = exec.LookPath("llama")
		if err != nil {
			return fmt.Errorf("can't find llama executable: %s", err.Error())
		}
	}

	cmd.Path = llama
	cmd.Args = []string{"llama", "invoke"}
	cmd.Args = append(cmd.Args, "-o", remap(comp.Output, wd))
	if comp.Flag.MF != "" {
		cmd.Args = append(cmd.Args, "-o", remap(comp.Flag.MF, wd))
	}
	cmd.Args = append(cmd.Args, "-f", remap(comp.Input, wd))
	for _, dep := range deps {
		cmd.Args = append(cmd.Args, "-f", remap(dep, wd))
	}

	cmd.Args = append(cmd.Args, cfg.Function, comp.Compiler())

	for _, inc := range comp.Includes {
		cmd.Args = append(cmd.Args, inc.Opt, toRemote(inc.Path, wd))
	}
	for _, def := range comp.Defs {
		cmd.Args = append(cmd.Args, def.Opt, def.Def)
	}
	cmd.Args = append(cmd.Args, "-c")
	cmd.Args = append(cmd.Args, "-o", toRemote(comp.Output, wd))
	cmd.Args = append(cmd.Args, toRemote(comp.Input, wd))
	if comp.Flag.MD {
		cmd.Args = append(cmd.Args, "-MD")
	}
	if comp.Flag.MMD {
		cmd.Args = append(cmd.Args, "-MMD")
	}
	if comp.Flag.MF != "" {
		cmd.Args = append(cmd.Args, "-MF", toRemote(comp.Flag.MF, wd))
	}
	cmd.Args = append(cmd.Args, comp.UnknownArgs...)
	if cfg.Verbose {
		log.Printf("[llamacc] compiling remotely: %q", cmd.Args)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
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
