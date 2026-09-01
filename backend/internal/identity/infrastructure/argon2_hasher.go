package infrastructure

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

type Argon2idHasher struct {
	params    Argon2idParams
	decoyHash string
}

const decoyPassword = "muse-decoy-credential-that-belongs-to-no-account-e3f1a9"

type Argon2idParams struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

var DefaultArgon2idParams = Argon2idParams{
	Memory:      19456,
	Iterations:  2,
	Parallelism: 1,
	SaltLength:  16,
	KeyLength:   32,
}

var ErrInvalidEncodedHash = errors.New("argon2: encoded hash is not in the expected format")

func NewArgon2idHasher(params Argon2idParams) *Argon2idHasher {
	h := &Argon2idHasher{params: params}
	decoySalt := make([]byte, params.SaltLength)
	h.decoyHash = h.encode(decoySalt, h.derive([]byte(decoyPassword), decoySalt))
	return h
}

func NewDefaultArgon2idHasher() *Argon2idHasher {
	return NewArgon2idHasher(DefaultArgon2idParams)
}

func (h *Argon2idHasher) Params() Argon2idParams { return h.params }

func (h *Argon2idHasher) Hash(password string) (string, error) {
	salt := make([]byte, h.params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("argon2: generate salt: %w", err)
	}
	return h.encode(salt, h.derive([]byte(password), salt)), nil
}

func (h *Argon2idHasher) derive(password, salt []byte) []byte {
	return argon2.IDKey(
		password,
		salt,
		h.params.Iterations,
		h.params.Memory,
		h.params.Parallelism,
		h.params.KeyLength,
	)
}

func (h *Argon2idHasher) encode(salt, key []byte) string {
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		h.params.Memory,
		h.params.Iterations,
		h.params.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
}

func (h *Argon2idHasher) VerifyDecoy(password string) {
	_, _, _ = h.Verify(password, h.decoyHash)
}

func (h *Argon2idHasher) DecoyHash() string { return h.decoyHash }

func (h *Argon2idHasher) Verify(password, encoded string) (bool, bool, error) {
	params, salt, expected, err := decodeArgon2idHash(encoded)
	if err != nil {
		return false, false, err
	}

	candidate := argon2.IDKey(
		[]byte(password),
		salt,
		params.Iterations,
		params.Memory,
		params.Parallelism,
		uint32(len(expected)),
	)

	if subtle.ConstantTimeCompare(candidate, expected) != 1 {
		return false, false, nil
	}
	return true, h.isWeakerThanCurrent(params), nil
}

func (h *Argon2idHasher) isWeakerThanCurrent(stored Argon2idParams) bool {
	return stored.Memory < h.params.Memory ||
		stored.Iterations < h.params.Iterations ||
		stored.KeyLength < h.params.KeyLength ||
		stored.SaltLength < h.params.SaltLength
}

func decodeArgon2idHash(encoded string) (Argon2idParams, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" {
		return Argon2idParams{}, nil, nil, ErrInvalidEncodedHash
	}
	if parts[1] != "argon2id" {
		return Argon2idParams{}, nil, nil, ErrInvalidEncodedHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return Argon2idParams{}, nil, nil, ErrInvalidEncodedHash
	}
	if version != argon2.Version {
		return Argon2idParams{}, nil, nil, ErrInvalidEncodedHash
	}

	var params Argon2idParams
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &params.Memory, &params.Iterations, &params.Parallelism); err != nil {
		return Argon2idParams{}, nil, nil, ErrInvalidEncodedHash
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return Argon2idParams{}, nil, nil, ErrInvalidEncodedHash
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return Argon2idParams{}, nil, nil, ErrInvalidEncodedHash
	}
	if len(salt) == 0 || len(key) == 0 {
		return Argon2idParams{}, nil, nil, ErrInvalidEncodedHash
	}

	params.SaltLength = uint32(len(salt))
	params.KeyLength = uint32(len(key))
	return params, salt, key, nil
}
