package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
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

	objfile, err := ioutil.TempFile(path.Dir(comp.Output), ".llama.*.o")
	if err != nil {
		return fmt.Errorf("tempfile: %w", err)
	}
	defer os.Remove(objfile.Name())

	comp_language := "cpp-output"
	if comp.Language != "c" {
		comp_language = comp.Language + "-cpp-output"
	}

	var compiler exec.Cmd
	compiler.Path = ccpath
	compiler.Args = []string{comp.Compiler()}
	compiler.Args = append(compiler.Args, comp.RemoteArgs...)
	compiler.Args = append(compiler.Args, "-x", comp_language, "-o", objfile.Name(), "-")
	compiler.Stderr = os.Stderr
	compiler.Stdin = &preprocessed

	if verbose {
		log.Printf("run %s: %q", comp.Compiler(), compiler.Args)
	}
	if err := compiler.Run(); err != nil {
		return err
	}

	return os.Rename(objfile.Name(), comp.Output)
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

	cmd := exec.Command(comp.Compiler(), os.Args[1:]...)
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
