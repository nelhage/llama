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
	"fmt"
	"io/ioutil"
	"log"
	"net/url"
	"path"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/klauspost/compress/zstd"
	"github.com/nelhage/llama/protocol"
	"github.com/nelhage/llama/store"
	"github.com/nelhage/llama/store/diskcache"
	"github.com/nelhage/llama/store/internal/storeutil"
	"github.com/nelhage/llama/tracing"
	"golang.org/x/sync/errgroup"
)

type Options struct {
	DisableHeadCheck bool
	DiskCachePath    string
	DiskCacheBytes   uint64
}

type Store struct {
	opts    Options
	session *session.Session
	s3      *s3.S3
	url     *url.URL

	seen storeutil.Cache
	disk *diskcache.Cache

	metricsMu sync.Mutex
	metrics   usageMetrics
}

type usageMetrics struct {
	ReadRequests  uint64
	WriteRequests uint64
	XferIn        uint64
	XferOut       uint64
}

var (
	encode *zstd.Encoder
	decode *zstd.Decoder
)

func init() {
	var err error
	encode, err = zstd.NewWriter(nil)
	if err != nil {
		panic(fmt.Sprintf("zstd: init writer: %s", err.Error()))
	}
	decode, err = zstd.NewReader(nil)
	if err != nil {
		panic(fmt.Sprintf("zstd: init reader: %s", err.Error()))
	}
}

func (s *Store) FetchAWSUsage(u *protocol.StoreUsage) {
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()
	u.Write_Requests += s.metrics.WriteRequests
	u.Read_Requests += s.metrics.ReadRequests
	u.Xfer_In += s.metrics.XferIn
	u.Xfer_Out += s.metrics.XferOut
	s.metrics = usageMetrics{}
}

func (s *Store) addUsage(add *usageMetrics) {
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()
	s.metrics.ReadRequests += add.ReadRequests
	s.metrics.WriteRequests += add.WriteRequests
	s.metrics.XferOut += add.XferOut
	s.metrics.XferIn += add.XferIn
}

func FromSession(s *session.Session, address string) (*Store, error) {
	return FromSessionAndOptions(s, address, Options{})
}

func FromSessionAndOptions(s *session.Session, address string, opts Options) (*Store, error) {
	u, e := url.Parse(address)
	if e != nil {
		return nil, fmt.Errorf("Parsing store: %q: %w", address, e)
	}
	if u.Scheme != "s3" {
		return nil, fmt.Errorf("Object store: %q: unsupported scheme %s", address, u.Scheme)
	}
	svc := s3.New(s, aws.NewConfig().WithS3DisableContentMD5Validation(true))
	svc.Handlers.Sign.PushFront(func(r *request.Request) {
		r.HTTPRequest.Header.Add("X-Amz-Content-Sha256", "UNSIGNED-PAYLOAD")
	})

	var disk *diskcache.Cache
	if opts.DiskCacheBytes > 0 {
		disk = diskcache.New(opts.DiskCachePath, opts.DiskCacheBytes)
	}

	return &Store{
		opts:    opts,
		session: s,
		s3:      svc,
		url:     u,
		disk:    disk,
	}, nil
}

func (s *Store) Store(ctx context.Context, obj []byte) (string, error) {
	ctx, span := tracing.StartSpan(ctx, "s3.store")
	defer span.End()
	id := storeutil.HashObject(obj) + ":zstd"

	span.AddField("object_id", id)
	if s.seen.HasObject(id) {
		return id, nil
	}

	key := aws.String(path.Join(s.url.Path, id))
	var err error

	var usage usageMetrics
	defer s.addUsage(&usage)

	upload := s.seen.StartUpload(id)
	defer upload.Rollback()

	if !s.opts.DisableHeadCheck {
		usage.ReadRequests += 1
		_, err = s.s3.HeadObjectWithContext(ctx, &s3.HeadObjectInput{
			Bucket: &s.url.Host,
			Key:    key,
		})
		if err == nil {
			upload.Complete()
			span.AddField("s3.exists", true)
			return id, nil
		}
		if reqerr, ok := err.(awserr.RequestFailure); ok && reqerr.StatusCode() == 404 {
			// 404 not found -- do the upload
		} else {
			return "", err
		}
	}

	compressed := encode.EncodeAll(obj, nil)
	span.AddField("s3.write_bytes", len(compressed))

	usage.WriteRequests += 1
	_, err = s.s3.PutObjectWithContext(ctx, &s3.PutObjectInput{
		Body:   bytes.NewReader(compressed),
		Bucket: &s.url.Host,
		Key:    key,
	})
	if err != nil {
		return "", err
	}
	s.metrics.XferIn += uint64(len(obj))
	upload.Complete()
	return id, nil
}

const getConcurrency = 32

func (s *Store) getFromS3(ctx context.Context, id string, usage *usageMetrics) ([]byte, error) {
	ctx, span := tracing.StartSpan(ctx, "s3.get_one")
	defer span.End()

	atomic.AddUint64(&usage.ReadRequests, 1)
	resp, err := s.s3.GetObjectWithContext(ctx, &s3.GetObjectInput{
		Bucket: &s.url.Host,
		Key:    aws.String(path.Join(s.url.Path, id)),
	})
	if err != nil {
		return nil, err
	}
	body, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}

	span.AddField("s3.read_bytes", len(body))
	atomic.AddUint64(&usage.XferOut, uint64(len(body)))

	if s.disk != nil {
		s.disk.Put(id, body)
	}
	return body, nil
}

func (s *Store) decompress(id string, body []byte) (string, []byte, error) {
	expectHash := id
	colon := strings.IndexRune(id, ':')
	if colon > 0 {
		expectHash = id[:colon]
		coding := id[colon+1:]
		if coding != "zstd" {
			return expectHash, nil, fmt.Errorf("%q: unknown compression %s", id, coding)
		}
		var err error
		body, err = decode.DecodeAll(body, nil)
		if err != nil {
			return expectHash, nil, fmt.Errorf("%q: decoding:  %w", id, err)
		}
	}
	return expectHash, body, nil
}

func (s *Store) getOne(ctx context.Context, id string, usage *usageMetrics) ([]byte, error) {
	var body []byte
	if s.disk != nil {
		body, _ = s.disk.Get(id)
	}
	if body == nil {
		var err error
		body, err = s.getFromS3(ctx, id, usage)
		if err != nil {
			return nil, err
		}
	}

	hash, body, err := s.decompress(id, body)
	if err != nil {
		return nil, err
	}

	gotHash := storeutil.HashObject(body)
	if gotHash != hash {
		return nil, fmt.Errorf("object store mismatch: got csum=%s expected %s", gotHash, id)
	}
	u := s.seen.StartUpload(id)
	u.Complete()

	return body, nil
}

func (s *Store) GetObjects(ctx context.Context, gets []store.GetRequest) {
	ctx, span := tracing.StartSpan(ctx, "s3.get_objects")
	defer span.End()
	span.AddField("objects", len(gets))
	grp, ctx := errgroup.WithContext(ctx)
	jobs := make(chan int)

	var usage usageMetrics
	defer s.addUsage(&usage)

	grp.Go(func() error {
		defer close(jobs)
		for i := range gets {
			jobs <- i
		}
		return nil
	})
	for i := 0; i < getConcurrency; i++ {
		grp.Go(func() error {
			for idx := range jobs {
				gets[idx].Data, gets[idx].Err = s.getOne(ctx, gets[idx].Id, &usage)
			}
			return nil
		})
	}

	if err := grp.Wait(); err != nil {
		log.Fatalf("GetObjects: internal error %s", err)
	}
}
