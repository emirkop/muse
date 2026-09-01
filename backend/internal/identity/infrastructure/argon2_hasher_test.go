package infrastructure_test

import (
	"errors"
	"strings"
	"testing"

	"muse-backend/internal/identity/infrastructure"
)

func TestArgon2idHasher_RoundTrip(t *testing.T) {
	hasher := infrastructure.NewDefaultArgon2idHasher()

	encoded, err := hasher.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	ok, needsRehash, err := hasher.Verify("correct horse battery staple", encoded)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !ok {
		t.Fatal("the correct password must verify")
	}
	if needsRehash {
		t.Fatal("a hash just produced by this hasher must not need rehashing")
	}
}

func TestArgon2idHasher_WrongPasswordFails(t *testing.T) {
	hasher := infrastructure.NewDefaultArgon2idHasher()
	encoded, err := hasher.Hash("the real password")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	ok, _, err := hasher.Verify("the wrong password", encoded)
	if err != nil {
		t.Fatalf("verify must not error on a wrong password: %v", err)
	}
	if ok {
		t.Fatal("a wrong password must not verify")
	}
}

func TestArgon2idHasher_StoresNothingReversible(t *testing.T) {
	hasher := infrastructure.NewDefaultArgon2idHasher()
	const password = "a-very-distinctive-passphrase"

	encoded, err := hasher.Hash(password)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	if strings.Contains(encoded, password) {
		t.Fatal("the encoded hash must not contain the password")
	}
	if !strings.HasPrefix(encoded, "$argon2id$") {
		t.Fatalf("expected a PHC argon2id string, got %q", encoded)
	}
}

func TestArgon2idHasher_SaltsAreUniquePerHash(t *testing.T) {
	hasher := infrastructure.NewDefaultArgon2idHasher()

	first, err := hasher.Hash("identical password")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	second, err := hasher.Hash("identical password")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	if first == second {
		t.Fatal("two hashes of the same password must differ — the salt must be per-hash and random")
	}
	for _, encoded := range []string{first, second} {
		ok, _, err := hasher.Verify("identical password", encoded)
		if err != nil || !ok {
			t.Fatalf("both hashes must verify: ok=%v err=%v", ok, err)
		}
	}
}

func TestArgon2idHasher_EncodesItsParameters(t *testing.T) {
	params := infrastructure.Argon2idParams{
		Memory: 8192, Iterations: 3, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	}
	encoded, err := infrastructure.NewArgon2idHasher(params).Hash("password one two")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	if !strings.Contains(encoded, "m=8192,t=3,p=1") {
		t.Fatalf("the hash must record the parameters it was made with, got %q", encoded)
	}
}

func TestArgon2idHasher_ReportsNeedsRehashForWeakerParameters(t *testing.T) {
	weak := infrastructure.NewArgon2idHasher(infrastructure.Argon2idParams{
		Memory: 8192, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	})
	current := infrastructure.NewDefaultArgon2idHasher()

	oldHash, err := weak.Hash("a stored password")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	ok, needsRehash, err := current.Verify("a stored password", oldHash)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !ok {
		t.Fatal("an old hash must still verify — raising the work factor must never lock anyone out")
	}
	if !needsRehash {
		t.Fatal("a hash made with weaker parameters must be reported as needing a rehash")
	}
}

func TestArgon2idHasher_DoesNotRehashStrongerHashes(t *testing.T) {
	strong := infrastructure.NewArgon2idHasher(infrastructure.Argon2idParams{
		Memory: 65536, Iterations: 4, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	})
	current := infrastructure.NewDefaultArgon2idHasher()

	strongHash, err := strong.Hash("a stored password")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	ok, needsRehash, err := current.Verify("a stored password", strongHash)
	if err != nil || !ok {
		t.Fatalf("a stronger hash must verify: ok=%v err=%v", ok, err)
	}
	if needsRehash {
		t.Fatal("a stronger hash must not be downgraded to the current parameters")
	}
}

func TestArgon2idHasher_MalformedHashIsAnError(t *testing.T) {
	hasher := infrastructure.NewDefaultArgon2idHasher()

	for name, encoded := range map[string]string{
		"empty":            "",
		"not phc":          "just-a-string",
		"bcrypt":           "$2y$10$abcdefghijklmnopqrstuv",
		"wrong algorithm":  "$argon2i$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA",
		"bad params":       "$argon2id$v=19$m=abc,t=2,p=1$c2FsdA$aGFzaA",
		"bad base64 salt":  "$argon2id$v=19$m=19456,t=2,p=1$!!!$aGFzaA",
		"missing sections": "$argon2id$v=19$m=19456,t=2,p=1$c2FsdA",
		"wrong version":    "$argon2id$v=16$m=19456,t=2,p=1$c2FsdA$aGFzaA",
	} {
		t.Run(name, func(t *testing.T) {
			ok, _, err := hasher.Verify("anything", encoded)
			if ok {
				t.Fatal("a malformed hash must never verify")
			}
			if !errors.Is(err, infrastructure.ErrInvalidEncodedHash) {
				t.Fatalf("expected ErrInvalidEncodedHash, got %v", err)
			}
		})
	}
}

func TestDefaultArgon2idParams_MeetRecordedGuidance(t *testing.T) {
	params := infrastructure.DefaultArgon2idParams

	if params.Memory < 19456 {
		t.Fatalf("memory cost %d KiB is below the recorded guidance (19456 KiB)", params.Memory)
	}
	if params.Iterations < 2 {
		t.Fatalf("iteration count %d is below the recorded guidance (2)", params.Iterations)
	}
	if params.SaltLength < 16 {
		t.Fatalf("salt length %d is below the 16-byte minimum", params.SaltLength)
	}
	if params.KeyLength < 32 {
		t.Fatalf("key length %d is below the 32-byte minimum", params.KeyLength)
	}
}

func BenchmarkArgon2idHasher_Hash(b *testing.B) {
	hasher := infrastructure.NewDefaultArgon2idHasher()
	for i := 0; i < b.N; i++ {
		if _, err := hasher.Hash("a representative passphrase"); err != nil {
			b.Fatalf("hash: %v", err)
		}
	}
}
