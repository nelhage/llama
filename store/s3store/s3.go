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

package s3store

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"net/url"
	"path"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"golang.org/x/crypto/blake2b"
)

type Store struct {
	session *session.Session
	s3      *s3.S3
	url     *url.URL
}

func FromSession(s *session.Session, address string) (*Store, error) {
	u, e := url.Parse(address)
	if e != nil {
		return nil, fmt.Errorf("Parsing store: %q: %w", address, e)
	}
	if u.Scheme != "s3" {
		return nil, fmt.Errorf("Object store: %q: unsupported scheme %s", address, u.Scheme)
	}
	return &Store{
		session: s,
		s3:      s3.New(s),
		url:     u,
	}, nil
}

func (s *Store) Store(ctx context.Context, obj []byte) (string, error) {
	csum := blake2b.Sum256(obj)
	id := hex.EncodeToString(csum[:])
	key := aws.String(path.Join(s.url.Path, id))
	_, err := s.s3.HeadObjectWithContext(ctx, &s3.HeadObjectInput{
		Bucket: &s.url.Host,
		Key:    key,
	})
	if err == nil {
		return id, nil
	}
	if reqerr, ok := err.(awserr.RequestFailure); ok && reqerr.StatusCode() == 404 {
		// 404 not found -- do the upload
	} else {
		return "", err
	}

	_, err = s.s3.PutObjectWithContext(ctx, &s3.PutObjectInput{
		Body:   bytes.NewReader(obj),
		Bucket: &s.url.Host,
		Key:    key,
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

func (s *Store) Get(ctx context.Context, id string) ([]byte, error) {
	resp, err := s.s3.GetObjectWithContext(ctx, &s3.GetObjectInput{
		Bucket: &s.url.Host,
		Key:    aws.String(path.Join(s.url.Path, id)),
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	gotSum := blake2b.Sum256(body)
	gotId := hex.EncodeToString(gotSum[:])
	if gotId != id {
		return nil, fmt.Errorf("object store mismatch: got csum=%s expected %s", gotId, id)
	}

	return body, nil
}
