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

package cli

import (
	"log"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/nelhage/llama/store"
	"github.com/nelhage/llama/store/s3store"
)

type GlobalState struct {
	mu      sync.Mutex
	session *session.Session

	Config *Config

	store store.Store
}

func (g *GlobalState) Session() (*session.Session, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.sessionLocked()
}

func (g *GlobalState) sessionLocked() (*session.Session, error) {
	if g.session != nil {
		return g.session, nil
	}
	awscfg := aws.NewConfig()
	if g.Config.Region != "" {
		awscfg = awscfg.WithRegion(g.Config.Region)
	}
	if g.Config.DebugAWS {
		awscfg = awscfg.WithLogLevel(aws.LogDebugWithHTTPBody)
	}
	var err error
	g.session, err = session.NewSession(awscfg)
	return g.session, err
}

func (g *GlobalState) MustSession() *session.Session {
	s, err := g.Session()
	if err != nil {
		log.Fatalf("llama: unable to initialize aws: %s", err.Error())
	}
	return s
}

func (g *GlobalState) Store() (store.Store, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.store != nil {
		return g.store, nil
	}
	sess, err := g.sessionLocked()
	if err != nil {
		return nil, err
	}
	opts := s3store.Options{
		DisableHeadCheck: true,
	}
	g.store, err = s3store.FromSessionAndOptions(sess, g.Config.Store, opts)
	if err != nil {
		return nil, err
	}
	return g.store, nil
}

func (g *GlobalState) MustStore() store.Store {
	st, err := g.Store()
	if err != nil {
		log.Fatalf("llama: initializing store: %s", err.Error())
	}
	return st
}
