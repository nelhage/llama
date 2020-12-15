package daemon

import (
	"context"
	"net/rpc"
)

func Dial(_ context.Context, path string) (*Client, error) {
	conn, err := rpc.DialHTTP("unix", path)
	if err != nil {
		return nil, err
	}
	return &Client{conn}, nil
}
