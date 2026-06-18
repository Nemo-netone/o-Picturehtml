package storage

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const defaultLocalRoot = "./data/recordings"

type LocalStorage struct {
	root string
}

// NewLocalStorage 创建基于本地目录的对象存储实现。
func NewLocalStorage(root string) *LocalStorage {
	root = strings.TrimSpace(root)
	if root == "" {
		root = defaultLocalRoot
	}
	return &LocalStorage{root: root}
}

func (s *LocalStorage) Put(ctx context.Context, key string, data []byte, options PutOptions) (Object, error) {
	if err := ctx.Err(); err != nil {
		return Object{}, err
	}
	checksum := SHA256(data)
	if options.Checksum != "" && options.Checksum != checksum {
		return Object{}, ErrChecksumMismatch
	}
	path, err := s.pathForKey(key)
	if err != nil {
		return Object{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Object{}, err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return Object{}, err
	}
	return objectWithoutDataCopy(Object{
		Key:         cleanObjectKey(key),
		ContentType: options.ContentType,
		Checksum:    checksum,
		Size:        int64(len(data)),
		Data:        append([]byte(nil), data...),
		CreatedAt:   time.Now(),
	}), nil
}

func (s *LocalStorage) Get(ctx context.Context, key string) (Object, error) {
	if err := ctx.Err(); err != nil {
		return Object{}, err
	}
	path, err := s.pathForKey(key)
	if err != nil {
		return Object{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Object{}, ErrNotFound
		}
		return Object{}, err
	}
	stat, err := os.Stat(path)
	if err != nil {
		return Object{}, err
	}
	return objectWithDataCopy(Object{
		Key:       cleanObjectKey(key),
		Checksum:  SHA256(data),
		Size:      int64(len(data)),
		Data:      data,
		CreatedAt: stat.ModTime(),
	}), nil
}

func (s *LocalStorage) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.pathForKey(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

func (s *LocalStorage) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	path, err := s.pathForKey(key)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrNotFound
		}
		return "", err
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	values := url.Values{}
	values.Set("expires", strconv.FormatInt(time.Now().Add(ttl).Unix(), 10))
	return "file://" + filepath.ToSlash(abs) + "?" + values.Encode(), nil
}

func (s *LocalStorage) pathForKey(key string) (string, error) {
	cleaned := cleanObjectKey(key)
	if cleaned == "" || strings.HasPrefix(cleaned, "../") || cleaned == ".." || filepath.IsAbs(cleaned) {
		return "", ErrNotFound
	}
	return filepath.Join(s.root, filepath.FromSlash(cleaned)), nil
}

func cleanObjectKey(key string) string {
	key = strings.ReplaceAll(strings.TrimSpace(key), "\\", "/")
	key = strings.TrimPrefix(filepath.ToSlash(filepath.Clean(key)), "./")
	if key == "." {
		return ""
	}
	return key
}
