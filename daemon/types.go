package daemon

import "github.com/nelhage/llama/files"

type PingArgs struct{}
type PingReply struct{}

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
