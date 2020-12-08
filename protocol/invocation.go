package protocol

type InvocationSpec struct {
	Args    []string        `json:"args"`
	Stdin   *Blob           `json:"stdin,omitempty"`
	Files   map[string]File `json:"files,omitempty"`
	Outputs []string        `json:"outputs,emitempty"`
}

type InvocationResponse struct {
	ExitStatus int             `json:"status"`
	Stdout     *Blob           `json:"stdout,omitempty"`
	Stderr     *Blob           `json:"stderr,omitempty"`
	Outputs    map[string]File `json:"outputs,omitempty"`
}
