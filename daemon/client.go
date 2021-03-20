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

func (c *Client) GetDaemonStats(in *StatsArgs) (*StatsReply, error) {
	var out StatsReply
	err := c.conn.Call("Daemon.GetDaemonStats", in, &out)
	return &out, err
}

func (c *Client) TraceSpans(in *TraceSpansArgs) (*TraceSpansReply, error) {
	var out TraceSpansReply
	err := c.conn.Call("Daemon.TraceSpans", in, &out)
	return &out, err
}

func (c *Client) PreloadPaths(in *PreloadPathsArgs) (*PreloadPathsResult, error) {
	var out PreloadPathsResult
	err := c.conn.Call("Daemon.PreloadPaths", in, &out)
	return &out, err
}
