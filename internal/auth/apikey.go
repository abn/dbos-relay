// Package auth mints and verifies the credentials Relay accepts.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
)

const (
	// KeyPrefix is confirmed by discovery item D4. Clients may validate it,
	// so it is part of the compatibility surface and not a Relay choice.
	KeyPrefix = "dbos_"

	// secretBytes is the entropy behind each key.
	secretBytes = 32

	// lookupLen is how much of the key is stored in clear to find the row.
	// It is not a secret and it is not sufficient to authenticate.
	lookupLen = 12
)

// KeyRecord is the persistable half of a minted key.
type KeyRecord struct {
	Lookup string
	Hash   []byte
}

// Mint generates a new key, returning the plaintext exactly once and the
// record to store. The plaintext is never persisted or logged.
func Mint() (string, KeyRecord, error) {
	raw := make([]byte, secretBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", KeyRecord{}, fmt.Errorf("generating key material: %w", err)
	}
	plain := KeyPrefix + base64.RawURLEncoding.EncodeToString(raw)
	return plain, KeyRecord{Lookup: Lookup(plain), Hash: hash(plain)}, nil
}

// Lookup returns the non-secret index for a key.
func Lookup(key string) string {
	if len(key) < lookupLen {
		return key
	}
	return key[:lookupLen]
}

// Verify reports whether key matches a stored hash, in constant time.
func Verify(key string, stored []byte) bool {
	return subtle.ConstantTimeCompare(hash(key), stored) == 1
}

// Hash returns the SHA-256 digest of a key string.
func Hash(key string) []byte {
	return hash(key)
}

func hash(key string) []byte {
	sum := sha256.Sum256([]byte(key))
	return sum[:]
}
