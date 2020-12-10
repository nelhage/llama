package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"strings"
)

func runLlamaCC(verbose bool, comp *Compilation) error {
	var err error
	var preprocessor exec.Cmd
	ccpath, err := exec.LookPath(comp.Compiler())
	if err != nil {
		return err
	}
	preprocessor.Path = ccpath
	preprocessor.Args = []string{comp.Compiler()}
	preprocessor.Args = append(preprocessor.Args, comp.LocalArgs...)
	preprocessor.Args = append(preprocessor.Args, "-E", "-o", "-", comp.Input)
	var preprocessed bytes.Buffer
	preprocessor.Stdout = &preprocessed
	preprocessor.Stderr = os.Stderr
	if verbose {
		log.Printf("run cpp: %q", preprocessor.Args)
	}
	if err := preprocessor.Run(); err != nil {
		return err
	}

	var compiler exec.Cmd
	if os.Getenv("LLAMACC_LOCAL") != "" {
		compiler.Path = ccpath
		compiler.Args = []string{comp.Compiler()}
		compiler.Args = append(compiler.Args, comp.RemoteArgs...)
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
		functionName := os.Getenv("LLAMACC_FUNCTION")
		if functionName == "" {
			functionName = "gcc"
		}
		compiler.Path = llama
		compiler.Args = []string{"llama", "invoke", "-o", comp.Output, "-stdin", functionName, comp.Compiler()}
		compiler.Args = append(compiler.Args, comp.RemoteArgs...)
		compiler.Args = append(compiler.Args, "-x", comp.PreprocessedLanguage, "-o", comp.Output, "-")
		compiler.Stderr = os.Stderr
		compiler.Stdin = &preprocessed
	}

	if verbose {
		log.Printf("run %s: %q", comp.Compiler(), compiler.Args)
	}
	if err := compiler.Run(); err != nil {
		return err
	}

	return nil
}

func main() {
	verbose := os.Getenv("LLAMACC_VERBOSE") != ""
	comp, err := ParseCompile(os.Args)
	if err == nil {
		err = runLlamaCC(verbose, &comp)
		if err != nil {
			if ex, ok := err.(*exec.ExitError); ok {
				os.Exit(ex.ExitCode())
			}
			fmt.Fprintf(os.Stderr, "Running gcc: %s\n", err.Error())
			os.Exit(1)
		}
		os.Exit(0)
	}
	if verbose {
		log.Printf("[llamacc] compiling locally: %s (%q)", err.Error(), os.Args)
	}

	cc := "gcc"
	if strings.HasSuffix(os.Args[0], "cxx") || strings.HasSuffix(os.Args[0], "c++") {
		cc = "g++"
	}

	cmd := exec.Command(cc, os.Args[1:]...)
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
