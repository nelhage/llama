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
	mt := tracing.NewMemoryTracer(ctx)
	ctx = tracing.WithTracer(ctx, mt)
	ctx, span := tracing.StartSpan(ctx, "llamacc")
	if cfg.BuildID != "" {
		span.AddField("global.build_id", cfg.BuildID)
	}

	client, err := server.DialWithAutostart(ctx, cli.SocketPath(), server.LlamaCCPath)
	if err != nil {
		return err
	}
	defer client.Close()

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
	args, err := constructRemotePreprocessInvoke(ctx, client, cfg, comp)
	if err != nil {
		return err
	}
	args.Trace = tracing.PropagationFromContext(ctx)
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

func constructRemotePreprocessInvoke(ctx context.Context, client *daemon.Client, cfg *Config, comp *Compilation) (*daemon.InvokeWithFilesArgs, error) {
	wd, err := files.WorkingDir()
	if err != nil {
		return nil, err
	}

	deps, err := detectDependencies(ctx, client, cfg, comp)
	if err != nil {
		return nil, fmt.Errorf("Detecting dependencies: %w", err)
	}

	args := daemon.InvokeWithFilesArgs{
		Function:      cfg.Function,
		DropSemaphore: true,
	}

	args.Outputs = args.Outputs.Append(remap(comp.Output, wd))

	if comp.Flag.MF != "" {
		args.Outputs = args.Outputs.Append(remap(comp.Flag.MF+".tmp", wd))
	}
	args.Files = args.Files.Append(remap(comp.Input, wd))
	for _, dep := range deps {
		args.Files = args.Files.Append(remap(dep, wd))
	}

	args.Args = []string{comp.RemoteCompiler(cfg)}

	if comp.Flag.SplitDwarf {
		args.Outputs = args.Outputs.Append(remap(replaceExt(comp.Output, ".dwo"), wd))
		args.Args = append(args.Args, "-gsplit-dwarf")
	}

	appendInclude := func(opt, local string) {
		mapped := toRemote(local, wd)
		args.Args = append(args.Args, opt, mapped)
		args.Args = append(args.Args, fmt.Sprintf("-fdebug-prefix-map=%s=%s", mapped, local))
	}

	appendInclude("-I", ".")
	for _, inc := range comp.Includes {
		appendInclude(inc.Opt, inc.Path)
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
	wd, err := files.WorkingDir()
	if err != nil {
		return err
	}
	ccpath, err := exec.LookPath(comp.LocalCompiler(cfg))
	if err != nil {
		return fmt.Errorf("find %s: %w", comp.LocalCompiler(cfg), err)
	}

	tmp, err := ioutil.TempFile("", fmt.Sprintf("llamacc-*%s", comp.LanguageExt()))
	if err != nil {
		return err
	}

	defer func() {
		os.Remove(tmp.Name())
		tmp.Close()
	}()

	var preprocessed bytes.Buffer
	{
		var preprocessor exec.Cmd
		_, span := tracing.StartSpan(ctx, "preprocess")
		preprocessor.Path = ccpath
		preprocessor.Args = []string{comp.LocalCompiler(cfg)}
		preprocessor.Args = append(preprocessor.Args, comp.LocalArgs...)
		if !cfg.FullPreprocess {
			preprocessor.Args = append(preprocessor.Args, "-fdirectives-only")

		}
		preprocessor.Args = append(preprocessor.Args, "-E", "-o", tmp.Name(), comp.Input)
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
		Files: []files.Mapped{
			remap(tmp.Name(), wd),
		},
		Outputs: []files.Mapped{
			{
				Local:  files.LocalFile{Path: path.Join(wd, comp.Output)},
				Remote: comp.Output,
			},
		},
		Stdin: preprocessed.Bytes(),
		Trace: tracing.PropagationFromContext(ctx),
	}
	args.Args = []string{comp.RemoteCompiler(cfg)}
	args.Args = append(args.Args, comp.RemoteArgs...)
	if !cfg.FullPreprocess {
		args.Args = append(args.Args, "-fdirectives-only", "-fpreprocessed")
	}
	args.Args = append(args.Args, "-x", comp.PreprocessedLanguage, "-o", comp.Output, toRemote(tmp.Name(), wd))

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
			if cfg.LocalFallback {
				goto RetryLocal
			} else if strings.Contains(err.Error(), "timed out") {
				goto RetryLocal
			} else {
				fmt.Fprintf(os.Stderr, "Running llamacc: %s\n", err.Error())
				os.Exit(1)
			}
		}
		os.Exit(0)
	}
RetryLocal:
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
		fmt.Fprintf(os.Stderr, "Running %s locally: %s\n", cc, err.Error())
		os.Exit(1)
	}
}
