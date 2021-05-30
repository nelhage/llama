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

package files

import (
	"fmt"
	"os"
	"path"
	"strings"
)

type IOContext struct {
	Inputs  List
	Outputs List
}

func (io *IOContext) cleanPath(file string) (Mapped, error) {
	if path.IsAbs(file) {
		return Mapped{}, fmt.Errorf("Cannot pass absolute path: %q", file)
	}
	file = path.Clean(file)
	if strings.HasPrefix(file, "../") {
		return Mapped{}, fmt.Errorf("Cannot pass path outside working directory: %q", file)
	}
	return Mapped{Local: LocalFile{Path: file}, Remote: file}, nil
}

func (io *IOContext) Input(file string) (string, error) {
	mapped, err := io.cleanPath(file)
	if err != nil {
		return "", err
	}
	io.Inputs = io.Inputs.Append(mapped)
	return mapped.Remote, nil
}

func (io *IOContext) I(file string) (string, error) {
	return io.Input(file)
}

func (io *IOContext) Output(file string) (string, error) {
	mapped, err := io.cleanPath(file)
	if err != nil {
		return "", err
	}
	io.Outputs = io.Outputs.Append(mapped)
	return mapped.Remote, nil
}

func (io *IOContext) O(file string) (string, error) {
	return io.Output(file)
}

func (io *IOContext) InputOutput(file string) (string, error) {
	mapped, err := io.cleanPath(file)
	if err != nil {
		return "", err
	}
	io.Inputs = io.Inputs.Append(mapped)
	io.Outputs = io.Outputs.Append(mapped)
	return mapped.Remote, nil
}

func (io *IOContext) IO(file string) (string, error) {
	return io.InputOutput(file)
}

// WorkingDir returns the absolute working directory of the process.
func WorkingDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	// If the working directory is set to the Linux procfs directory
	// (Bazel sandboxing does this), we need to resolve it to the real
	// directory so that it can be passed across to the daemon, which
	// may have a different working directory.
	if wd == "/proc/self/cwd" {
		return os.Readlink(wd)
	}

	return wd, err
}
