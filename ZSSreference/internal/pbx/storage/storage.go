//  PBX存储层
package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"strconv"
	"sync"
	"time"
)

var (
	ErrNotFound         = errors.New("object not found")
	ErrChecksumMismatch = errors.New("object checksum mismatch")
)

type PutOptions struct {
	ContentType string
	Checksum    string
}

type Object struct {
	Key         string
	ContentType string
	Checksum    string
	Size        int64
	Data        []byte
	CreatedAt   time.Time
}

type ObjectStorage interface {
	Put(ctx context.Context, key string, data []byte, options PutOptions) (Object, error)
	Get(ctx context.Context, key string) (Object, error)
	Delete(ctx context.Context, key string) error
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
}

type MemoryStorage struct {
	mu      sync.Mutex
	objects map[string]Object
}

// NewMemoryStorage 创建内存对象存储。
func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{objects: map[string]Object{}}
}

// Put 存储对象，可选校验 SHA-256 checksum。
func (s *MemoryStorage) Put(ctx context.Context, key string, data []byte, options PutOptions) (Object, error) {
	if err := ctx.Err(); err != nil {
		return Object{}, err
	}
	checksum := SHA256(data)
	if options.Checksum != "" && options.Checksum != checksum {
		return Object{}, ErrChecksumMismatch
	}
	object := Object{
		Key:         key,
		ContentType: options.ContentType,
		Checksum:    checksum,
		Size:        int64(len(data)),
		Data:        append([]byte(nil), data...),
		CreatedAt:   time.Now(),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = object
	return objectWithoutDataCopy(object), nil
}

// Get 读取对象。
func (s *MemoryStorage) Get(ctx context.Context, key string) (Object, error) {
	if err := ctx.Err(); err != nil {
		return Object{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	object, ok := s.objects[key]
	if !ok {
		return Object{}, ErrNotFound
	}
	return objectWithDataCopy(object), nil
}

// Delete 删除对象。
func (s *MemoryStorage) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.objects[key]; !ok {
		return ErrNotFound
	}
	delete(s.objects, key)
	return nil
}

// PresignGet 生成预签名 URL（模拟 S3 presigned URL）。
func (s *MemoryStorage) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	s.mu.Lock()
	_, ok := s.objects[key]
	s.mu.Unlock()
	if !ok {
		return "", ErrNotFound
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	values := url.Values{}
	values.Set("expires", strconv.FormatInt(time.Now().Add(ttl).Unix(), 10))
	return "memory://" + key + "?" + values.Encode(), nil
}

// SHA256 计算数据的十六进制 SHA-256 摘要。
func SHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// objectWithDataCopy 返回含数据副本的 Object。
func objectWithDataCopy(object Object) Object {
	object.Data = append([]byte(nil), object.Data...)
	return object
}

// objectWithoutDataCopy 返回不含数据副本的 Object。
func objectWithoutDataCopy(object Object) Object {
	object.Data = append([]byte(nil), object.Data...)
	return object
}

