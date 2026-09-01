package infrastructure

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"muse-backend/internal/media/application"
	"muse-backend/internal/platform/objectstore"
)

type FilesystemObjectStorage struct {
	root    string
	baseURL string
	secret  []byte
	now     func() time.Time
}

const DevStoragePathPrefix = "/dev-storage/"

func NewFilesystemObjectStorage(root, publicBaseURL string, secret []byte) (*FilesystemObjectStorage, error) {
	if root == "" || publicBaseURL == "" || len(secret) == 0 {
		return nil, errors.New("filesystem_object_storage: root, public base URL, and secret are required")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("filesystem_object_storage: create root: %w", err)
	}
	return &FilesystemObjectStorage{
		root:    root,
		baseURL: strings.TrimRight(publicBaseURL, "/"),
		secret:  secret,
		now:     time.Now,
	}, nil
}

type objectMeta struct {
	ContentType    string `json:"content_type"`
	ChecksumSHA256 string `json:"checksum_sha256"`
}

func (s *FilesystemObjectStorage) PresignUpload(_ context.Context, req application.PresignUploadRequest) (application.UploadTicket, error) {
	expires := s.now().Add(req.TTL)
	u := s.signedURL(http.MethodPut, req.Key, expires, req.ContentType, req.ByteSize, req.ChecksumSHA256)
	return application.UploadTicket{
		URL:       u,
		Method:    http.MethodPut,
		Headers:   map[string]string{"Content-Type": req.ContentType},
		ExpiresAt: expires,
	}, nil
}

func (s *FilesystemObjectStorage) Stat(_ context.Context, key string) (application.ObjectStat, error) {
	info, err := os.Stat(s.objectPath(key))
	if errors.Is(err, os.ErrNotExist) {
		return application.ObjectStat{}, application.ErrObjectNotFound
	}
	if err != nil {
		return application.ObjectStat{}, err
	}
	meta, err := s.readMeta(key)
	if err != nil {
		return application.ObjectStat{}, err
	}
	return application.ObjectStat{
		ByteSize:       info.Size(),
		ContentType:    meta.ContentType,
		ChecksumSHA256: meta.ChecksumSHA256,
	}, nil
}

func (s *FilesystemObjectStorage) ReadRange(_ context.Context, key string, offset, length int64) ([]byte, error) {
	f, err := os.Open(s.objectPath(key))
	if errors.Is(err, os.ErrNotExist) {
		return nil, application.ErrObjectNotFound
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	return io.ReadAll(io.LimitReader(f, length))
}

func (s *FilesystemObjectStorage) Open(_ context.Context, key string) (io.ReadCloser, error) {
	f, err := os.Open(s.objectPath(key))
	if errors.Is(err, os.ErrNotExist) {
		return nil, application.ErrObjectNotFound
	}
	return f, err
}

func (s *FilesystemObjectStorage) PresignDownload(_ context.Context, key string, ttl time.Duration) (application.DownloadTicket, error) {
	expires := s.now().Add(ttl)
	return application.DownloadTicket{
		URL:       s.signedURL(http.MethodGet, key, expires, "", 0, ""),
		ExpiresAt: expires,
	}, nil
}

func (s *FilesystemObjectStorage) Delete(_ context.Context, key string) error {
	for _, p := range []string{s.objectPath(key), s.metaPath(key)} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

var _ application.ObjectStorage = (*FilesystemObjectStorage)(nil)

// MARK: - Publishing platform assets

var _ objectstore.PublicWriter = (*FilesystemObjectStorage)(nil)

func (s *FilesystemObjectStorage) PutObject(_ context.Context, key, contentType string, body io.Reader, size int64, checksumSHA256 string) error {
	if strings.Contains(key, "..") {
		return errors.New("filesystem_object_storage: invalid key")
	}
	destination := s.objectPath(key)
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".publish-*")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())

	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(temporary, hash), io.LimitReader(body, size+1))
	closeErr := temporary.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if written != size {
		return fmt.Errorf("filesystem_object_storage: wrote %d bytes, declared %d", written, size)
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != checksumSHA256 {
		return fmt.Errorf("filesystem_object_storage: body checksum %s does not match declared %s", got, checksumSHA256)
	}
	if err := os.Rename(temporary.Name(), destination); err != nil {
		return err
	}
	return s.writeMeta(key, objectMeta{ContentType: contentType, ChecksumSHA256: checksumSHA256})
}

func (s *FilesystemObjectStorage) StatObject(ctx context.Context, key string) (objectstore.Stat, error) {
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

const DevPublicAssetPathPrefix = "/dev-assets/"

const PublicAssetKeyPrefix = "bundles/"

func (s *FilesystemObjectStorage) PublicAssetHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, DevPublicAssetPathPrefix)
		if key == "" || strings.Contains(key, "..") || !strings.HasPrefix(key, PublicAssetKeyPrefix) {
			http.NotFound(w, r)
			return
		}
		meta, err := s.readMeta(key)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", meta.ContentType)
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		http.ServeFile(w, r, s.objectPath(key))
	})
}

func (s *FilesystemObjectStorage) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, DevStoragePathPrefix)
		if key == "" || strings.Contains(key, "..") {
			http.Error(w, "invalid key", http.StatusBadRequest)
			return
		}
		q := r.URL.Query()
		if !s.verifySignature(r.Method, key, q) {
			http.Error(w, "signature invalid or expired", http.StatusForbidden)
			return
		}

		switch r.Method {
		case http.MethodPut:
			s.handlePut(w, r, key, q)
		case http.MethodGet:
			s.handleGet(w, r, key)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}

func (s *FilesystemObjectStorage) handlePut(w http.ResponseWriter, r *http.Request, key string, q url.Values) {
	wantType := q.Get("ct")
	wantLen, _ := strconv.ParseInt(q.Get("len"), 10, 64)
	wantSum := q.Get("sum")

	if r.Header.Get("Content-Type") != wantType {
		http.Error(w, "content-type does not match the signed value", http.StatusForbidden)
		return
	}
	if r.ContentLength >= 0 && r.ContentLength != wantLen {
		http.Error(w, "content-length does not match the signed value", http.StatusForbidden)
		return
	}

	dst := s.objectPath(key)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".upload-*")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.Remove(tmp.Name())

	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(r.Body, wantLen+1))
	tmp.Close()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if written != wantLen {
		http.Error(w, "body length does not match the signed value", http.StatusBadRequest)
		return
	}
	gotSum := hex.EncodeToString(hash.Sum(nil))
	if gotSum != wantSum {
		http.Error(w, "body checksum does not match the signed value", http.StatusBadRequest)
		return
	}

	if err := os.Rename(tmp.Name(), dst); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.writeMeta(key, objectMeta{ContentType: wantType, ChecksumSHA256: gotSum}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *FilesystemObjectStorage) handleGet(w http.ResponseWriter, r *http.Request, key string) {
	meta, err := s.readMeta(key)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", meta.ContentType)
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeFile(w, r, s.objectPath(key))
}

// MARK: - Signing (dev semantics of a presigned URL)

func (s *FilesystemObjectStorage) signedURL(method, key string, expires time.Time, contentType string, size int64, checksum string) string {
	q := url.Values{}
	q.Set("exp", strconv.FormatInt(expires.Unix(), 10))
	if method == http.MethodPut {
		q.Set("ct", contentType)
		q.Set("len", strconv.FormatInt(size, 10))
		q.Set("sum", checksum)
	}
	q.Set("sig", s.sign(method, key, q))
	return s.baseURL + DevStoragePathPrefix + key + "?" + q.Encode()
}

func (s *FilesystemObjectStorage) verifySignature(method, key string, q url.Values) bool {
	exp, err := strconv.ParseInt(q.Get("exp"), 10, 64)
	if err != nil || s.now().Unix() > exp {
		return false
	}
	presented := q.Get("sig")
	unsigned := url.Values{}
	for k, v := range q {
		if k != "sig" {
			unsigned[k] = v
		}
	}
	return hmac.Equal([]byte(presented), []byte(s.sign(method, key, unsigned)))
}

func (s *FilesystemObjectStorage) sign(method, key string, q url.Values) string {
	mac := hmac.New(sha256.New, s.secret)
	fmt.Fprintf(mac, "%s\n%s\n%s\n%s\n%s\n%s", method, key, q.Get("exp"), q.Get("ct"), q.Get("len"), q.Get("sum"))
	return hex.EncodeToString(mac.Sum(nil))
}

// MARK: - Paths

func (s *FilesystemObjectStorage) objectPath(key string) string {
	return filepath.Join(s.root, filepath.FromSlash(path.Clean("/"+key)))
}

func (s *FilesystemObjectStorage) metaPath(key string) string {
	return s.objectPath(key) + ".meta.json"
}

func (s *FilesystemObjectStorage) readMeta(key string) (objectMeta, error) {
	raw, err := os.ReadFile(s.metaPath(key))
	if err != nil {
		return objectMeta{}, err
	}
	var meta objectMeta
	return meta, json.Unmarshal(raw, &meta)
}

func (s *FilesystemObjectStorage) writeMeta(key string, meta objectMeta) error {
	raw, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(s.metaPath(key), raw, 0o644)
}
