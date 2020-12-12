package daemon

import "net/rpc"

type Client struct {
	conn *rpc.Client
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) Ping(in *PingArgs) (*PingReply, error) {
	var out PingReply
	err := c.conn.Call("Daemon.Ping", in, &out)
	return &out, err
}

func (c *Client) Shutdown(in *ShutdownArgs) (*ShutdownReply, error) {
	var out ShutdownReply
	err := c.conn.Call("Daemon.Shutdown", in, &out)
	return &out, err
}

func (c *Client) InvokeWithFiles(in *InvokeWithFilesArgs) (*InvokeWithFilesReply, error) {
	var out InvokeWithFilesReply
	err := c.conn.Call("Daemon.InvokeWithFiles", in, &out)
	return &out, err
}
