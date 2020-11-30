package protocol

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"unicode/utf8"

	"github.com/nelhage/llama/store"
)

const MaxInlineBlob = 10 * 1024

type Blob struct {
	String string `json:"s,omitempty"`
	Bytes  []byte `json:"b,omitempty"`
	Ref    string `json:"r,omitempty"`
	Err    string `json:"e,omitempty"`
}

type Arg struct {
	In  *Blob   `json:"i,omitempty"`
	Out *string `json:"o,omitempty"`
}

type File struct {
	Blob
	Mode os.FileMode `json:"m"`
}

type InvocationSpec struct {
	Args  []json.RawMessage `json:"args"`
	Stdin *Blob             `json:"stdin,omitempty"`
	Files map[string]File   `json:"files"`
}

type InvocationResponse struct {
	ExitStatus int             `json:"status"`
	Stdout     *Blob           `json:"stdout,omitempty"`
	Stderr     *Blob           `json:"stderr,omitempty"`
	Outputs    map[string]Blob `json:"outputs,omitempty"`
}

func (b *Blob) Read(ctx context.Context, store store.Store) ([]byte, error) {
	if b.String != "" {
		return []byte(b.String), nil
	}
	if b.Bytes != nil {
		return b.Bytes, nil
	}
	if b.Ref != "" {
		return store.Get(ctx, b.Ref)
	}
	return nil, nil
}

func NewBlob(ctx context.Context, store store.Store, bytes []byte) (*Blob, error) {
	stringOk := utf8.Valid(bytes)
	if stringOk && len(bytes) < MaxInlineBlob {
		return &Blob{String: string(bytes)}, nil
	}
	if base64.StdEncoding.EncodedLen(len(bytes)) < MaxInlineBlob {
		return &Blob{Bytes: bytes}, nil
	}
	id, err := store.Store(ctx, bytes)
	if err != nil {
		return nil, err
	}
	return &Blob{Ref: id}, nil
}
