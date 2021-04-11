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
	"context"
	"fmt"
	"log"
	"os"

	"github.com/nelhage/llama/daemon"
	"github.com/nelhage/llama/tracing"
)

func detectDependencies(ctx context.Context, client *daemon.Client, cfg *Config, comp *Compilation) ([]string, error) {
	_, span := tracing.StartSpan(ctx, "detect_dependencies")
	defer span.End()

	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	cpp := &daemon.RunCPPArgs{
		Trace: span.Propagation(),
		Dir:   cwd,
		Cmd:   comp.Compiler(),
	}

	cpp.Args = append(cpp.Args, comp.UnknownArgs...)
	for _, opt := range comp.Defs {
		cpp.Args = append(cpp.Args, opt.Opt)
		cpp.Args = append(cpp.Args, opt.Def)
	}
	for _, opt := range comp.Includes {
		cpp.Args = append(cpp.Args, opt.Opt)
		cpp.Args = append(cpp.Args, opt.Path)
	}
	cpp.Args = append(cpp.Args, "-MM", "-MF", "-", comp.Input)
	if cfg.Verbose {
		log.Printf("run cpp -MM: %q", cpp.Args)
	}
	out, err := client.RunCPP(cpp)
	if err != nil {
		return nil, err
	}
	if out.Status != 0 {
		os.Stderr.Write(out.Stderr)
		return nil, fmt.Errorf("cpp exited with code %d", out.Status)
	}
	return out.Deps, nil
}
