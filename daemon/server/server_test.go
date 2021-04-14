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

package server_test

import (
	"context"
	"log"
	"os"
	"os/exec"
	"path"
	"sort"
	"sync"
	"testing"

	"github.com/nelhage/llama/daemon"
	"github.com/nelhage/llama/daemon/server"
)

func TestDialWithAutostart(t *testing.T) {
	if _, err := exec.LookPath("llama"); err != nil {
		t.Skip("Need a llama binary in the path to run autostart tests")
	}
	dir := t.TempDir()
	sock := path.Join(dir, "llama.sock")
	type result struct {
		i   int
		pid int
		err error
	}
	ch := make(chan result)
	start := make(chan struct{})
	ctx := context.Background()

	os.Setenv("LLAMA_DIR", dir)
	os.Setenv("LLAMA_OBJECT_STORE", "s3://dummy-store/")

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			r := result{i: i}
			defer func() {
				ch <- r
			}()
			cl, err := server.DialWithAutostart(ctx, sock, "/")
			if err != nil {
				r.err = err
				return
			}
			pong, err := cl.Ping(&daemon.PingArgs{})
			if err != nil {
				r.err = err
				return
			}
			r.pid = pong.ServerPid
			cl.Close()
		}(i)
	}
	go func() {
		defer close(ch)
		wg.Wait()
	}()
	close(start)
	var results []result
	for r := range ch {
		if r.err != nil {
			t.Errorf("client %d exited with error: %s",
				r.i, r.err.Error())
		}
		results = append(results, r)
	}
	if cl, err := daemon.Dial(ctx, sock); err == nil {
		cl.Shutdown(&daemon.ShutdownArgs{})
		cl.Close()
	}
	// TODO: It is possible that, after we shutdown the server,
	// one of the autostarted servers is still racing to start up,
	// notices there is no server, and grabs the socket. I don't
	// currently see a clean way to prevent this.

	sort.Slice(results, func(i, j int) bool {
		return results[i].i < results[j].i
	})
	pid := results[0].pid
	log.Printf("server started sock=%s pid=%d", sock, pid)
	for i, r := range results {
		if r.pid != pid {
			t.Errorf("client %d got pid: %d != %d", i, r.pid, pid)
		}
		if r.i != i {
			t.Errorf("results[%d] = %d", i, r.i)
		}
	}

}
