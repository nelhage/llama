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

package protocol

type FileAndPath struct {
	File
	Path string `json:"p"`
}
type FileList []FileAndPath

type InvocationSpec struct {
	Args    []string `json:"args"`
	Stdin   *Blob    `json:"stdin,omitempty"`
	Files   FileList `json:"files,omitempty"`
	Outputs []string `json:"outputs,emitempty"`
}

type InvocationResponse struct {
	ExitStatus int      `json:"status"`
	Stdout     *Blob    `json:"stdout,omitempty"`
	Stderr     *Blob    `json:"stderr,omitempty"`
	Outputs    FileList `json:"outputs,omitempty"`
}
