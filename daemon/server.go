package daemon

import "context"

type Daemon struct {
	shutdown context.CancelFunc
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
