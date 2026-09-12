package auth_test

import (
	"strings"
	"testing"

	"github.com/abn/relay/internal/auth"
)

func TestMintProducesDistinctVerifiableKeys(t *testing.T) {
	seen := make(map[string]bool)
	for range 100 {
		plain, rec, err := auth.Mint()
		if err != nil {
			t.Fatalf("Mint: %v", err)
		}
		if seen[plain] {
			t.Fatal("Mint produced a duplicate key")
		}
		seen[plain] = true

		if !strings.HasPrefix(plain, auth.KeyPrefix) {
			t.Errorf("key %q lacks the %q prefix", plain, auth.KeyPrefix)
		}
		if !auth.Verify(plain, rec.Hash) {
			t.Error("Verify rejected the key it just minted")
		}
		if auth.Verify(plain+"x", rec.Hash) {
			t.Error("Verify accepted a modified key")
		}
		if rec.Lookup != auth.Lookup(plain) {
			t.Errorf("Lookup mismatch: record %q, function %q", rec.Lookup, auth.Lookup(plain))
		}
	}
}

func TestHashIsNotThePlaintext(t *testing.T) {
	plain, rec, err := auth.Mint()
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if strings.Contains(string(rec.Hash), plain) {
		t.Fatal("the stored hash contains the plaintext key")
	}
}

func TestLookup_ShortKeyDoesNotReturnPlaintext(t *testing.T) {
	shortKeys := []string{"", "a", "local", "dbos_secret", "12345678901"}
	for _, k := range shortKeys {
		lookup := auth.Lookup(k)
		if lookup == k && len(k) > 0 {
			t.Errorf("Lookup(%q) returned plaintext key", k)
		}
		if lookup != "" {
			t.Errorf("Lookup(%q) = %q, want empty string for sub-12 char key", k, lookup)
		}
	}
}
