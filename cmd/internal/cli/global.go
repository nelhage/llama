package cli

import (
	"context"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/nelhage/llama/store"
)

type GlobalState struct {
	Session *session.Session
	Region  string
	Bucket  string

	Store store.Store
}

type key int

var globalKey key

func WithState(ctx context.Context, state *GlobalState) context.Context {
	return context.WithValue(ctx, globalKey, state)
}

func GetState(ctx context.Context) (*GlobalState, bool) {
	v, ok := ctx.Value(globalKey).(*GlobalState)
	return v, ok
}

func MustState(ctx context.Context) *GlobalState {
	state, ok := GetState(ctx)
	if !ok {
		panic("MustState: no CLI state present")
	}
	return state
}
