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

import (
	"context"
	"net/rpc"
)

func Dial(_ context.Context, sockPath string) (*Client, error) {
	conn, err := rpc.DialHTTP("unix", sockPath)
	if err != nil {
		return nil, err
	}
	return &Client{conn}, nil
}

func DialPath(_ context.Context, sockPath string, urlPath string) (*Client, error) {
	conn, err := rpc.DialHTTPPath("unix", sockPath, urlPath)
	if err != nil {
		return nil, err
	}
	return &Client{conn}, nil
}
