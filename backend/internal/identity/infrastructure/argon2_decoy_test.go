package infrastructure_test

import (
	"sort"
	"strings"
	"testing"
	"time"

	"muse-backend/internal/identity/infrastructure"
)

func TestArgon2idHasher_DecoyCarriesTheCurrentParameters(t *testing.T) {
	hasher := infrastructure.NewArgon2idHasher(infrastructure.Argon2idParams{
		Memory: 8192, Iterations: 3, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	})

	decoy := hasher.DecoyHash()

	if !strings.HasPrefix(decoy, "$argon2id$") {
		t.Fatalf("the decoy must be a real PHC argon2id string, got %q", decoy)
	}
	if !strings.Contains(decoy, "m=8192,t=3,p=1") {
		t.Fatalf("the decoy must carry the hasher's own parameters, got %q", decoy)
	}
}

func TestArgon2idHasher_DecoyTracksParameterChanges(t *testing.T) {
	weak := infrastructure.NewArgon2idHasher(infrastructure.Argon2idParams{
		Memory: 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	})
	strong := infrastructure.NewArgon2idHasher(infrastructure.Argon2idParams{
		Memory: 4096, Iterations: 2, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	})

	if weak.DecoyHash() == strong.DecoyHash() {
		t.Fatal("hashers with different parameters must have different decoys, or a raised work factor would not raise the decoy's cost")
	}
}

func TestArgon2idHasher_DecoyIsAWellFormedHashForTheRealVerifyPath(t *testing.T) {
	hasher := infrastructure.NewArgon2idHasher(infrastructure.Argon2idParams{
		Memory: 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	})

	ok, needsRehash, err := hasher.Verify("not-the-decoy-input", hasher.DecoyHash())

	if err != nil {
		t.Fatalf("the decoy must decode cleanly through the real path, got %v", err)
	}
	if ok {
		t.Fatal("an arbitrary password must not match the decoy")
	}
	if needsRehash {
		t.Fatal("the decoy is made with the current parameters and must never read as needing a rehash")
	}
}

func TestArgon2idHasher_VerifyDecoyIsSafeToCallRepeatedly(t *testing.T) {
	hasher := infrastructure.NewArgon2idHasher(infrastructure.Argon2idParams{
		Memory: 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	})

	for _, password := range []string{"", "x", strings.Repeat("p", 128), "muse-decoy-credential-that-belongs-to-no-account-e3f1a9"} {
		hasher.VerifyDecoy(password)
	}
}

func TestArgon2idHasher_VerifyDecoyCostsComparablyToARealVerification(t *testing.T) {
	if testing.Short() {
		t.Skip("production-parameter timing check skipped in -short mode")
	}
	hasher := infrastructure.NewDefaultArgon2idHasher()
	realHash, err := hasher.Hash("the-real-passphrase")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	const rounds = 7
	realDurations := make([]time.Duration, 0, rounds)
	decoyDurations := make([]time.Duration, 0, rounds)

	for i := 0; i < rounds; i++ {
		start := time.Now()
		_, _, _ = hasher.Verify("wrong-passphrase", realHash)
		realDurations = append(realDurations, time.Since(start))

		start = time.Now()
		hasher.VerifyDecoy("wrong-passphrase")
		decoyDurations = append(decoyDurations, time.Since(start))
	}

	realMedian := median(realDurations)
	decoyMedian := median(decoyDurations)

	if decoyMedian < realMedian/3 {
		t.Fatalf(
			"decoy verification is far cheaper than a real one (decoy %v vs real %v): "+
				"the not-found path would be distinguishable by timing",
			decoyMedian, realMedian,
		)
	}
	t.Logf("real verify median %v, decoy verify median %v (this machine, not the production host)", realMedian, decoyMedian)
}

func median(durations []time.Duration) time.Duration {
	sorted := append([]time.Duration(nil), durations...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted[len(sorted)/2]
}
