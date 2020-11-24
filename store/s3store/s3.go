package s3store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/ioutil"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

type Store struct {
	session *session.Session
	s3      *s3.S3
	bucket  string
}

func FromSession(s *session.Session, bucket string) *Store {
	return &Store{
		session: s,
		s3:      s3.New(s),
		bucket:  bucket,
	}
}

func (s *Store) Store(ctx context.Context, obj []byte) (string, error) {
	sha := sha256.Sum256(obj)
	id := hex.EncodeToString(sha[:])
	_, err := s.s3.PutObjectWithContext(ctx, &s3.PutObjectInput{
		Body:   bytes.NewReader(obj),
		Bucket: &s.bucket,
		Key:    aws.String(fmt.Sprintf("obj/%s", id)),
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

func (s *Store) Get(ctx context.Context, id string) ([]byte, error) {
	resp, err := s.s3.GetObjectWithContext(ctx, &s3.GetObjectInput{
		Bucket: &s.bucket,
		Key:    aws.String(fmt.Sprintf("obj/%s", id)),
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	gotSha := sha256.Sum256(body)
	gotId := hex.EncodeToString(gotSha[:])
	if gotId != id {
		return nil, fmt.Errorf("object store mismatch: got sha=%s expected sha=%s", gotId, id)
	}

	return body, nil
}
