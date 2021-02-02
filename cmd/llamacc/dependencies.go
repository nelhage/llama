package main

import (
	"bytes"
	"context"
	"log"
	"os"
	"os/exec"
)

func detectDependencies(ctx context.Context, cfg *Config, comp *Compilation) ([]string, error) {
	var preprocessor exec.Cmd
	ccpath, err := exec.LookPath(comp.Compiler())
	if err != nil {
		return nil, err
	}
	preprocessor.Path = ccpath
	preprocessor.Args = []string{comp.Compiler()}
	preprocessor.Args = append(preprocessor.Args, comp.UnknownArgs...)
	for _, opt := range comp.Defs {
		preprocessor.Args = append(preprocessor.Args, opt.Opt)
		preprocessor.Args = append(preprocessor.Args, opt.Def)
	}
	for _, opt := range comp.Includes {
		preprocessor.Args = append(preprocessor.Args, opt.Opt)
		preprocessor.Args = append(preprocessor.Args, opt.Path)
	}
	preprocessor.Args = append(preprocessor.Args, "-fdirectives-only")
	preprocessor.Args = append(preprocessor.Args, "-MM", "-MF", "-", comp.Input)
	var deps bytes.Buffer
	preprocessor.Stdout = &deps
	preprocessor.Stderr = os.Stderr
	if cfg.Verbose {
		log.Printf("run cpp -MM: %q", preprocessor.Args)
	}
	if err := preprocessor.Run(); err != nil {
		return nil, err
	}
	return parseMakeDeps(deps.Bytes())
}

func parseMakeDeps(buf []byte) ([]string, error) {
	var deps []string
	i := 0
	// Skip the target
	for i < len(buf) && buf[i] != ':' {
		i++
	}
	i++

	var dep []byte
	for i < len(buf) {
		if buf[i] == ' ' || buf[i] == '\n' {
			if len(dep) > 0 {
				deps = append(deps, string(dep))
			}
			dep = dep[:0]
			i++
			continue
		}
		if buf[i] == '\\' && i+1 < len(buf) {
			if buf[i+1] == '\n' {
				i++
				continue
			}
			if buf[i+1] == ' ' || buf[i+1] == '\\' {
				dep = append(dep, buf[i+1])
				i += 2
				continue
			}
		}
		dep = append(dep, buf[i])
		i++
	}
	if len(dep) > 0 {
		deps = append(deps, string(dep))
	}

	return deps, nil
}
