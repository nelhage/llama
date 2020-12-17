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

package daemon

import "github.com/nelhage/llama/files"

type PingArgs struct{}
type PingReply struct {
	ServerPid int
}

type ShutdownArgs struct{}
type ShutdownReply struct{}

type InvokeWithFilesArgs struct {
	Function   string
	ReturnLogs bool
	Args       []string
	Stdin      []byte
	Files      files.List
	Outputs    files.List
}

type InvokeWithFilesReply struct {
	InvokeErr  string
	ExitStatus int
	Stdout     []byte
	Stderr     []byte
	Logs       []byte
}
