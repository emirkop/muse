package infrastructure

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	"muse-backend/internal/media/application"
	"muse-backend/internal/platform/objectstore"
)

type R2ObjectStorage struct {
	client    *s3.Client
	presigner *s3.PresignClient
	bucket    string
	now       func() time.Time
}

type R2Config struct {
	AccountID       string
	Endpoint        string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
}

func NewR2ObjectStorage(cfg R2Config) (*R2ObjectStorage, error) {
	if cfg.Bucket == "" || cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return nil, errors.New("r2_object_storage: bucket, access key id, and secret access key are required")
	}
	endpoint := cfg.Endpoint
	if endpoint == "" {
		if cfg.AccountID == "" {
			return nil, errors.New("r2_object_storage: account id (or an explicit endpoint) is required")
		}
		endpoint = fmt.Sprintf("https://%s.r2.cloudflarestorage.com", cfg.AccountID)
	}

	client := s3.New(s3.Options{
		Region:                     "auto",
		BaseEndpoint:               aws.String(endpoint),
		Credentials:                credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		UsePathStyle:               true,
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	})

	return &R2ObjectStorage{
		client:    client,
		presigner: s3.NewPresignClient(client),
		bucket:    cfg.Bucket,
		now:       time.Now,
	}, nil
}

func (s *R2ObjectStorage) PresignUpload(ctx context.Context, req application.PresignUploadRequest) (application.UploadTicket, error) {
	checksumB64, err := hexToBase64(req.ChecksumSHA256)
	if err != nil {
		return application.UploadTicket{}, fmt.Errorf("r2_object_storage: checksum: %w", err)
	}

	presigned, err := s.presigner.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:         aws.String(s.bucket),
		Key:            aws.String(req.Key),
		ContentType:    aws.String(req.ContentType),
		ContentLength:  aws.Int64(req.ByteSize),
		ChecksumSHA256: aws.String(checksumB64),
	}, s3.WithPresignExpires(req.TTL))
	if err != nil {
		return application.UploadTicket{}, fmt.Errorf("r2_object_storage: presign put: %w", err)
	}

	return application.UploadTicket{
		URL:       presigned.URL,
		Method:    presigned.Method,
		Headers:   flattenHeaders(presigned.SignedHeader),
		ExpiresAt: s.now().Add(req.TTL),
	}, nil
}

func (s *R2ObjectStorage) Stat(ctx context.Context, key string) (application.ObjectStat, error) {
	head, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket:       aws.String(s.bucket),
		Key:          aws.String(key),
		ChecksumMode: s3types.ChecksumModeEnabled,
	})
	if err != nil {
		if isNotFound(err) {
			return application.ObjectStat{}, application.ErrObjectNotFound
		}
		return application.ObjectStat{}, fmt.Errorf("r2_object_storage: head: %w", err)
	}

	stat := application.ObjectStat{
		ByteSize:    aws.ToInt64(head.ContentLength),
		ContentType: aws.ToString(head.ContentType),
	}
	if head.ChecksumSHA256 != nil {
		if hexed, err := base64ToHex(*head.ChecksumSHA256); err == nil {
			stat.ChecksumSHA256 = hexed
		}
	}
	return stat, nil
}

func (s *R2ObjectStorage) ReadRange(ctx context.Context, key string, offset, length int64) ([]byte, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Range:  aws.String(fmt.Sprintf("bytes=%d-%d", offset, offset+length-1)),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, application.ErrObjectNotFound
		}
		return nil, fmt.Errorf("r2_object_storage: get range: %w", err)
	}
	defer out.Body.Close()
	return io.ReadAll(io.LimitReader(out.Body, length))
}

func (s *R2ObjectStorage) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, application.ErrObjectNotFound
		}
		return nil, fmt.Errorf("r2_object_storage: get: %w", err)
	}
	return out.Body, nil
}

func (s *R2ObjectStorage) PresignDownload(ctx context.Context, key string, ttl time.Duration) (application.DownloadTicket, error) {
	presigned, err := s.presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return application.DownloadTicket{}, fmt.Errorf("r2_object_storage: presign get: %w", err)
	}
	return application.DownloadTicket{URL: presigned.URL, ExpiresAt: s.now().Add(ttl)}, nil
}

func (s *R2ObjectStorage) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("r2_object_storage: delete: %w", err)
	}
	return nil
}

var _ application.ObjectStorage = (*R2ObjectStorage)(nil)

// MARK: - Publishing platform assets

var _ objectstore.PublicWriter = (*R2ObjectStorage)(nil)

func (s *R2ObjectStorage) PutObject(ctx context.Context, key, contentType string, body io.Reader, size int64, checksumSHA256 string) error {
	checksumB64, err := hexToBase64(checksumSHA256)
	if err != nil {
		return fmt.Errorf("r2_object_storage: checksum: %w", err)
	}
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:         aws.String(s.bucket),
		Key:            aws.String(key),
		Body:           body,
		ContentType:    aws.String(contentType),
		ContentLength:  aws.Int64(size),
		ChecksumSHA256: aws.String(checksumB64),
		CacheControl:   aws.String("public, max-age=31536000, immutable"),
	})
	if err != nil {
		return fmt.Errorf("r2_object_storage: put: %w", err)
	}
	return nil
}

func (s *R2ObjectStorage) StatObject(ctx context.Context, key string) (objectstore.Stat, error) {
	stat, err := s.Stat(ctx, key)
	if err != nil {
		if errors.Is(err, application.ErrObjectNotFound) {
			return objectstore.Stat{}, objectstore.ErrObjectNotFound
		}
		return objectstore.Stat{}, err
	}
	return objectstore.Stat{
		ByteSize:       stat.ByteSize,
		ContentType:    stat.ContentType,
		ChecksumSHA256: stat.ChecksumSHA256,
	}, nil
}

// MARK: - Helpers

func isNotFound(err error) bool {
	var noSuchKey *s3types.NoSuchKey
	var notFound *s3types.NotFound
	return errors.As(err, &noSuchKey) || errors.As(err, &notFound)
}

func flattenHeaders(header http.Header) map[string]string {
	out := make(map[string]string, len(header))
	for name, values := range header {
		if len(values) > 0 {
			out[name] = values[0]
		}
	}
	return out
}

func hexToBase64(hexed string) (string, error) {
	raw, err := hex.DecodeString(hexed)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

func base64ToHex(b64 string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}
