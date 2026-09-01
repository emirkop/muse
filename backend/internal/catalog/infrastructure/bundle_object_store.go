package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"muse-backend/internal/catalog/application"
	"muse-backend/internal/catalog/domain"
	"muse-backend/internal/platform/objectstore"
)

type BundleObjectStore struct {
	writer        objectstore.PublicWriter
	publicBaseURL string
}

func NewBundleObjectStore(writer objectstore.PublicWriter, publicBaseURL string) (*BundleObjectStore, error) {
	if writer == nil {
		return nil, errors.New("catalog: bundle object store requires an object writer")
	}
	trimmed := strings.TrimRight(publicBaseURL, "/")
	if trimmed == "" {
		return nil, errors.New("catalog: bundle object store requires a public base URL")
	}
	return &BundleObjectStore{writer: writer, publicBaseURL: trimmed}, nil
}

var _ application.BundleObjectStore = (*BundleObjectStore)(nil)

func (s *BundleObjectStore) Put(ctx context.Context, key, contentType string, body io.Reader, size int64, checksumSHA256 string) error {
	if err := assertBundleKey(key); err != nil {
		return err
	}
	return s.writer.PutObject(ctx, key, contentType, body, size, checksumSHA256)
}

func (s *BundleObjectStore) Stat(ctx context.Context, key string) (application.StoredObject, error) {
	if err := assertBundleKey(key); err != nil {
		return application.StoredObject{}, err
	}
	stat, err := s.writer.StatObject(ctx, key)
	if err != nil {
		return application.StoredObject{}, err
	}
	return application.StoredObject{ByteSize: stat.ByteSize, ChecksumSHA256: stat.ChecksumSHA256}, nil
}

func (s *BundleObjectStore) PublicURL(key string) string {
	return s.publicBaseURL + "/" + key
}

func assertBundleKey(key string) error {
	if !strings.HasPrefix(key, domain.BundleStorageKeyPrefix) || strings.Contains(key, "..") {
		return fmt.Errorf("catalog: %q is not an asset bundle key", key)
	}
	return nil
}
