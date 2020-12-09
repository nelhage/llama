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
)

type Compilation struct {
	Language   string
	Input      string
	Output     string
	LocalArgs  []string
	RemoteArgs []string
	Flag       Flags
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

var argExts = map[string]bool{
	".c":   true,
	".cxx": true,
	".cc":  true,
	".cpp": true,
}

func smellsLikeInput(arg string) bool {
	ext := path.Ext(arg)
	return argExts[ext]

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
		c.Language = arg
		return argAction{err: errors.New("-S given")}
	}, true},
	{"-o", func(c *Compilation, arg string) argAction {
		if c.Output != "" {
			return argAction{err: fmt.Errorf("multiple outputs: %s, %s", c.Output, arg)}
		}
		c.Output = arg
		return argAction{filterRemote: true, filterLocal: true}
	}, true},
}

func ParseCompile(argv []string) (Compilation, error) {
	var out Compilation
	cmd := argv[0]
	args := argv[1:]

	if strings.HasSuffix(cmd, "cc") {
		out.Language = "c"
	} else if strings.HasSuffix(cmd, "cxx") || strings.HasSuffix(cmd, "c++") {
		out.Language = "c++"
	}

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
						if i >= len(args) {
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
		ext := path.Ext(out.Input)
		out.Output = out.Input[:len(out.Input)-len(ext)] + ".o"
	}

	return out, nil
}

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

	var compiler exec.Cmd
	compiler.Path = ccpath
	compiler.Args = []string{comp.Compiler()}
	compiler.Args = append(compiler.Args, comp.RemoteArgs...)
	compiler.Args = append(compiler.Args, "-x", comp.Language, "-o", objfile.Name(), "-")
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
