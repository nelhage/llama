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
	"io/ioutil"
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
		span.AddField("global.build_id", cfg.BuildID)
	}
	defer func() {
		span.End()
		client.TraceSpans(&daemon.TraceSpansArgs{Spans: mt.Close()})
	}()

	if cfg.LocalPreprocess {
		return buildLocalPreprocess(ctx, client, cfg, comp)
	} else {
		return buildRemotePreprocess(ctx, client, cfg, comp)
	}
}

func toAbs(local, wd string) string {
	if path.IsAbs(local) {
		return local
	}
	return path.Join(wd, local)
}

func toRemote(local, wd string) string {
	return path.Join("_root", toAbs(local, wd))
}

func remap(local, wd string) files.Mapped {
	return files.Mapped{
		Local: files.LocalFile{
			Path: toAbs(local, wd),
		},
		Remote: toRemote(local, wd),
	}
}

func buildRemotePreprocess(ctx context.Context, client *daemon.Client, cfg *Config, comp *Compilation) error {
	preload := make(chan struct{})
	// buildRemoteInvoke will take some time to run `gcc` locally
	// in order to discover dependencies. While it runs, we can at
	// least start uploading the .c file we _know_ we will need to
	// upload.
	//
	// During a build, we will likely eventually have uploaded all
	// the header files, which means that this input file may be
	// the _only_ source file we need to upload, and so getting a
	// head-start on it can be a substantial performance
	// improvement.
	go func() {
		defer close(preload)
		_, span := tracing.StartSpan(ctx, "preload")
		defer span.End()
		client.PreloadPaths(&daemon.PreloadPathsArgs{Paths: []string{comp.Input}})
	}()
	args, err := buildRemoteInvoke(ctx, cfg, comp)
	if err != nil {
		return err
	}
	args.Trace = tracing.PropagationFromContext(ctx)
	<-preload
	out, err := client.InvokeWithFiles(args)
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

	if comp.Flag.MF != "" {
		return rewriteMF(ctx, comp)
	}

	return nil
}

func rewriteMF(ctx context.Context, comp *Compilation) error {
	tmpMF := comp.Flag.MF + ".tmp"
	data, err := ioutil.ReadFile(tmpMF)
	if err != nil {
		return err
	}
	data = bytes.ReplaceAll(data, []byte("_root/"), []byte("/"))
	if err := ioutil.WriteFile(comp.Flag.MF, data, 0644); err != nil {
		return err
	}
	return os.Remove(tmpMF)
}

func buildRemoteInvoke(ctx context.Context, cfg *Config, comp *Compilation) (*daemon.InvokeWithFilesArgs, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	deps, err := detectDependencies(ctx, cfg, comp)
	if err != nil {
		return nil, fmt.Errorf("Detecting dependencies: %w", err)
	}

	args := daemon.InvokeWithFilesArgs{
		Function: cfg.Function,
	}

	args.Outputs = args.Outputs.Append(remap(comp.Output, wd))

	if comp.Flag.MF != "" {
		args.Outputs = args.Outputs.Append(remap(comp.Flag.MF+".tmp", wd))
	}
	args.Files = args.Files.Append(remap(comp.Input, wd))
	for _, dep := range deps {
		args.Files = args.Files.Append(remap(dep, wd))
	}

	args.Args = []string{comp.Compiler()}

	args.Args = append(args.Args, "-I", toRemote(".", wd))
	for _, inc := range comp.Includes {
		args.Args = append(args.Args, inc.Opt, toRemote(inc.Path, wd))
	}
	for _, def := range comp.Defs {
		args.Args = append(args.Args, def.Opt, def.Def)
	}
	args.Args = append(args.Args, "-c")
	args.Args = append(args.Args, "-o", toRemote(comp.Output, wd))
	args.Args = append(args.Args, toRemote(comp.Input, wd))
	if comp.Flag.MD {
		args.Args = append(args.Args, "-MD")
	}
	if comp.Flag.MMD {
		args.Args = append(args.Args, "-MMD")
	}
	if comp.Flag.MP {
		args.Args = append(args.Args, "-MP")
	}
	if comp.Flag.MF != "" {
		args.Args = append(args.Args, "-MF", toRemote(comp.Flag.MF+".tmp", wd))
	}
	args.Args = append(args.Args, comp.UnknownArgs...)
	if cfg.Verbose {
		log.Printf("[llamacc] compiling remotely: %#v", args)
	}
	return &args, nil
}

func buildLocalPreprocess(ctx context.Context, client *daemon.Client, cfg *Config, comp *Compilation) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	ccpath, err := exec.LookPath(comp.Compiler())
	if err != nil {
		return fmt.Errorf("find %s: %w", comp.Compiler(), err)
	}

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
		Trace: tracing.PropagationFromContext(ctx),
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

	cc := cfg.LocalCC
	if strings.HasSuffix(os.Args[0], "cxx") || strings.HasSuffix(os.Args[0], "c++") {
		cc = cfg.LocalCXX
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
