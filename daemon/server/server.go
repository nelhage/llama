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

package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"os/exec"
	"path"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/gofrs/flock"
	"github.com/nelhage/llama/daemon"
	"github.com/nelhage/llama/store"
)

type Daemon struct {
	ctx context.Context

	shutdown context.CancelFunc
	store    store.Store
	session  *session.Session
	lambda   *lambda.Lambda

	stats daemon.Stats

	cppSem chan (struct{})
}

var ErrAlreadyRunning = errors.New("daemon already running")

type StartArgs struct {
	Path        string
	Store       store.Store
	Session     *session.Session
	IdleTimeout time.Duration
}

// TODO: ncpu
const concurrentCppLimit = 8

func Start(ctx context.Context, args *StartArgs) error {
	if err := os.MkdirAll(path.Dir(args.Path), 0700); err != nil {
		return err
	}

	lk := flock.New(args.Path + ".lock")
	ok, err := lk.TryLock()
	if err != nil {
		return err
	}
	if !ok {
		return ErrAlreadyRunning
	}
	defer lk.Unlock()

	// Unlink the socket if it already exists. We have the
	// exclusive lock, so we know no one is listening.
	os.Remove(args.Path)
	listener, err := net.Listen("unix", args.Path)

	if err != nil {
		return err
	}

	srvCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	daemon := Daemon{
		ctx:      srvCtx,
		shutdown: cancel,
		store:    args.Store,
		session:  args.Session,
		lambda:   lambda.New(args.Session),
		cppSem:   make(chan struct{}, concurrentCppLimit),
	}

	extend := make(chan struct{})
	go func() {
		waitForIdle(srvCtx, extend, args.IdleTimeout)
		cancel()
	}()

	var httpSrv http.Server
	var rpcSrv rpc.Server
	rpcSrv.Register(&daemon)
	httpSrv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		extend <- struct{}{}
		rpcSrv.ServeHTTP(w, r)
	})
	go func() {
		httpSrv.Serve(listener)
	}()
	<-srvCtx.Done()

	httpSrv.Shutdown(ctx)
	return nil
}

func DialWithAutostart(ctx context.Context, path string) (*daemon.Client, error) {
	cl, err := daemon.Dial(ctx, path)
	if err == nil {
		return cl, nil
	}
	cmd := exec.Command("llama", "daemon", "-autostart", "-path", path)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	exitStatus := make(chan error)
	connected := make(chan *daemon.Client)
	shutdown := make(chan struct{})
	go func() {
		defer close(exitStatus)
		exitStatus <- cmd.Wait()
	}()
	go func() {
		for {
			cl, err := daemon.Dial(ctx, path)
			if err == nil {
				connected <- cl
				return
			}
			select {
			case <-shutdown:
				return
			case <-time.After(10 * time.Millisecond):
				// Try again
			}
		}
	}()
	for {
		select {
		case cl = <-connected:
			return cl, nil
		case err := <-exitStatus:
			if err == nil {
				// The autostart exited 0, so someone
				// else must have raced to autostart.
				exitStatus = nil
				break
			}
			// Stop the goroutine that's trying to connect
			close(shutdown)
			return nil, fmt.Errorf("Starting server: %s", err.Error())
		}
	}
}

func waitForIdle(srvCtx context.Context, extend chan struct{}, timeout time.Duration) {
	var timer *time.Timer
	var expire <-chan time.Time
	if timeout != 0 {
		timer = time.NewTimer(timeout)
		expire = timer.C
	}
loop:
	for {
		select {
		case <-srvCtx.Done():
			break loop
		case <-expire:
			break loop
		case <-extend:
			if timer != nil {
				if !timer.Stop() {
					<-timer.C
				}
				timer.Reset(timeout)
				expire = timer.C
			}
		}
	}
	if timer != nil {
		timer.Stop()
	}
}
