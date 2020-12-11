package daemon

type Daemon struct {
}

type PingInput struct{}
type PingReply struct{}

func (d *Daemon) Ping(in PingInput, reply *PingReply) error {
	*reply = PingReply{}
	return nil
}
