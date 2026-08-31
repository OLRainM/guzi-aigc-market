package asset

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"

	"github.com/minio/minio-go/v7"
)

var errObjectNotFound = errors.New("object not found")

type ObjectInfo struct {
	Size        int64
	ContentType string
}

type ObjectStore interface {
	EnsureBucket(ctx context.Context) error
	Put(ctx context.Context, key, contentType string, body io.Reader, size int64) error
	Get(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error)
	Delete(ctx context.Context, key string) error
}

type MinIOStore struct {
	client *minio.Client
	bucket string
}

func NewMinIOStore(client *minio.Client, bucket string) *MinIOStore {
	return &MinIOStore{client: client, bucket: bucket}
}

func (s *MinIOStore) EnsureBucket(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{})
}

func (s *MinIOStore) Put(ctx context.Context, key, contentType string, body io.Reader, size int64) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, body, size, minio.PutObjectOptions{ContentType: contentType})
	return err
}

func (s *MinIOStore) Get(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	object, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	stat, err := object.Stat()
	if err != nil {
		_ = object.Close()
		return nil, ObjectInfo{}, err
	}
	return object, ObjectInfo{Size: stat.Size, ContentType: stat.ContentType}, nil
}

func (s *MinIOStore) Delete(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}

type MemoryStore struct {
	mu      sync.Mutex
	bucket  string
	objects map[string]memoryObject
}

type memoryObject struct {
	contentType string
	data        []byte
}

func NewMemoryStore(bucket string) *MemoryStore {
	return &MemoryStore{bucket: bucket, objects: map[string]memoryObject{}}
}

func (s *MemoryStore) EnsureBucket(context.Context) error { return nil }

func (s *MemoryStore) Put(_ context.Context, key, contentType string, body io.Reader, size int64) error {
	data, err := io.ReadAll(io.LimitReader(body, size+1))
	if err != nil {
		return err
	}
	if int64(len(data)) != size {
		return ErrInvalidFile
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = memoryObject{contentType: contentType, data: data}
	return nil
}

func (s *MemoryStore) Get(_ context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	s.mu.Lock()
	object, ok := s.objects[key]
	s.mu.Unlock()
	if !ok {
		return nil, ObjectInfo{}, errObjectNotFound
	}
	return io.NopCloser(bytes.NewReader(object.data)), ObjectInfo{Size: int64(len(object.data)), ContentType: object.contentType}, nil
}

func (s *MemoryStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, key)
	return nil
}
