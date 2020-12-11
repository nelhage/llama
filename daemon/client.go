package daemon

import "net/rpc"

type Client struct {
	conn *rpc.Client
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) Ping(in *PingInput) (*PingReply, error) {
	var out PingReply
	err := c.conn.Call("Daemon.Ping", in, &out)
	return &out, err
}
