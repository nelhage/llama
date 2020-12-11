package daemon

import (
	"context"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/nelhage/llama/store"
)

type Daemon struct {
	shutdown context.CancelFunc
	store    store.Store
	session  *session.Session
	lambda   *lambda.Lambda
}

type PingArgs struct{}
type PingReply struct{}

func (d *Daemon) Ping(in PingArgs, reply *PingReply) error {
	*reply = PingReply{}
	return nil
}

type ShutdownArgs struct{}
type ShutdownReply struct{}

func (d *Daemon) Shutdown(in ShutdownArgs, out *ShutdownReply) error {
	d.shutdown()
	*out = ShutdownReply{}
	return nil
}
