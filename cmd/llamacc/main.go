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

	"context"

	"github.com/nelhage/llama/cmd/internal/cli"
	"github.com/nelhage/llama/daemon"
	"github.com/nelhage/llama/daemon/server"
	"github.com/nelhage/llama/files"
	"github.com/nelhage/llama/tracing"
)

func runLlamaCC(cfg *Config, comp *Compilation) error {
	var err error
	ccpath, err := exec.LookPath(comp.Compiler())
	wd, err := os.Getwd()
	ctx := context.Background()
	client, err := server.DialWithAutostart(ctx, cli.SocketPath())
	if err != nil {
		return err
	}
	defer client.Close()

	mt := tracing.NewMemoryTracer(ctx)
	ctx = tracing.WithTracer(ctx, mt)
	ctx, span := tracing.StartSpan(ctx, "llamacc")
	if cfg.BuildID != "" {
		span.SetLabel("build_id", cfg.BuildID)
	}
	defer func() {
		span.End()
		client.TraceSpans(&daemon.TraceSpansArgs{Spans: mt.Close()})
	}()

	var preprocessed bytes.Buffer
	{
		var preprocessor exec.Cmd
		_, span := tracing.StartSpan(ctx, "preprocess")
		preprocessor.Path = ccpath
		preprocessor.Args = []string{comp.Compiler()}
		preprocessor.Args = append(preprocessor.Args, comp.LocalArgs...)
		if !cfg.FullPreprocess {
			preprocessor.Args = append(preprocessor.Args, "-fdirectives-only")
		}
		preprocessor.Args = append(preprocessor.Args, "-E", "-o", "-", comp.Input)
		preprocessor.Stdout = &preprocessed
		preprocessor.Stderr = os.Stderr
		if cfg.Verbose {
			log.Printf("run cpp: %q", preprocessor.Args)
		}
		if err := preprocessor.Run(); err != nil {
			return err
		}
		span.End()
	}

	args := daemon.InvokeWithFilesArgs{
		Function: cfg.Function,
		Outputs: []files.Mapped{
			{
				Local:  files.LocalFile{Path: path.Join(wd, comp.Output)},
				Remote: comp.Output,
			},
		},
		Stdin: preprocessed.Bytes(),
	}
	args.Args = []string{comp.Compiler()}
	args.Args = append(args.Args, comp.RemoteArgs...)
	if !cfg.FullPreprocess {
		args.Args = append(args.Args, "-fdirectives-only", "-fpreprocessed")
	}
	args.Args = append(args.Args, "-x", comp.PreprocessedLanguage, "-o", comp.Output, "-")

	out, err := client.InvokeWithFiles(&args)
	if err != nil {
		return err
	}
	os.Stdout.Write(out.Stdout)
	os.Stderr.Write(out.Stderr)
	if out.InvokeErr != "" {
		return fmt.Errorf("invoke: %s", out.InvokeErr)
	}
	if out.ExitStatus != 0 {
		return fmt.Errorf("invoke: exit %d", out.ExitStatus)
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
	var err error
	var comp Compilation
	if cfg.Local {
		err = errors.New("LLAMACC_LOCAL set")
	}
	if err == nil {
		comp, err = ParseCompile(&cfg, os.Args)
	}
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
