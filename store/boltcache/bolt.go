package boltcache

import (
	"context"
	"encoding/binary"
	"log"
	"strings"
	"time"

	"github.com/nelhage/llama/protocol"
	"github.com/nelhage/llama/store"
	"github.com/nelhage/llama/store/internal/storeutil"
	bolt "go.etcd.io/bbolt"
)

type WriteCache struct {
	inner store.StorePrehashed
	db    *bolt.DB
	mem   storeutil.Cache
}

var _ store.StorePrehashed = &WriteCache{}

var idBucket = []byte("ids")
var cacheTTL = 7 * 24 * time.Hour

func NewWriteCache(inner store.StorePrehashed, path string) (*WriteCache, error) {
	db, err := bolt.Open(path, 0644, nil)
	if err != nil {
		return nil, err
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(idBucket)
		return err
	}); err != nil {
		return nil, err
	}
	return &WriteCache{
		inner: inner,
		db:    db,
	}, nil
}

func (w *WriteCache) Store(ctx context.Context, obj []byte) (string, error) {
	hash := w.inner.HashObject(obj)
	return w.StorePrehashed(ctx, obj, hash)
}

func (w *WriteCache) StorePrehashed(ctx context.Context, obj []byte, hash string) (string, error) {
	if w.mem.HasObject(hash) {
		return hash, nil
	}

	u := w.mem.StartUpload(hash)
	defer u.Rollback()
	got, err := w.inner.StorePrehashed(ctx, obj, hash)
	if err == nil {
		u.Complete()
		go func() {
			err := w.putBolt(ctx, hash)
			if err != nil {
				log.Printf("error writing key: %s", err.Error())
			} else {
				w.mem.RemoveObject(hash)
			}
		}()
	}
	return got, err
}

func (w *WriteCache) HashObject(obj []byte) string {
	return w.inner.HashObject(obj)
}

func (w *WriteCache) GetObjects(ctx context.Context, gets []store.GetRequest) {
	w.inner.GetObjects(ctx, gets)
	for i := range gets {
		if gets[i].Err != nil {
			continue
		}
		hash := gets[i].Id
		idx := strings.IndexRune(hash, ':')
		if idx > 0 {
			hash = hash[:idx]
		}
		u := w.mem.StartUpload(hash)
		u.Complete()
	}
}

func (w *WriteCache) FetchAWSUsage(u *protocol.UsageMetrics) {
	w.inner.FetchAWSUsage(u)
}

func (w *WriteCache) putBolt(ctx context.Context, id string) error {
	now := time.Now()
	var nowbytes [8]byte
	binary.BigEndian.PutUint64(nowbytes[:], uint64(now.Unix()))
	return w.db.Batch(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(idBucket)
		return bucket.Put([]byte(id), nowbytes[:])
	})
}

func (w *WriteCache) hasObject(ctx context.Context, id string) bool {
	var tm time.Time
	w.db.View(func(tx *bolt.Tx) error {
		got := tx.Bucket(idBucket).Get([]byte(id))
		if got == nil {
			return nil
		}
		tm = time.Unix(int64(binary.BigEndian.Uint64(got)), 0)
		return nil
	})
	return time.Since(tm) < cacheTTL
}
