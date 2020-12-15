package server_test

import (
	"context"
	"os"
	"os/exec"
	"path"
	"sort"
	"sync"
	"testing"

	"github.com/nelhage/llama/cmd/llama/internal/server"
	"github.com/nelhage/llama/daemon"
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

	os.Setenv("LLAMA_OBJECT_STORE", "s3://dummy-bucket/")

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
			cl, err := server.DialWithAutostart(ctx, sock)
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
		wg.Wait()
		close(ch)
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
	sort.Slice(results, func(i, j int) bool {
		return results[i].i < results[j].i
	})
	pid := results[0].pid
	for i, r := range results {
		if r.pid != pid {
			t.Errorf("client %d got pid: %d != %d", i, r.pid, pid)
		}
		if r.i != i {
			t.Errorf("results[%d] = %d", i, r.i)
		}
	}

}
