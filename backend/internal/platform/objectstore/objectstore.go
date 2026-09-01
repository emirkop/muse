package objectstore

import (
	"context"
	"errors"
	"io"
)

type Stat struct {
	ByteSize       int64
	ContentType    string
	ChecksumSHA256 string
}

var ErrObjectNotFound = errors.New("objectstore: no object at key")

type PublicWriter interface {
	PutObject(ctx context.Context, key, contentType string, body io.Reader, size int64, checksumSHA256 string) error

	StatObject(ctx context.Context, key string) (Stat, error)
}
