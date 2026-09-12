package auth

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	stdhash "hash"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	ErrKeySetUnavailable = errors.New("OIDC key set unavailable (failed closed)")
	ErrInvalidToken      = errors.New("invalid or malformed token")
	ErrTokenExpired      = errors.New("token is expired")
	ErrTokenNotYetValid  = errors.New("token is not yet valid")
	ErrIssuerMismatch    = errors.New("token issuer does not match configured issuer")
	ErrAudienceMismatch  = errors.New("token audience does not match configured audience")
	ErrUnknownKey        = errors.New("signing key not found in key set")
)

// Audience handles unmarshaling single strings or string arrays in JWT claims.
type Audience []string

func (a *Audience) UnmarshalJSON(data []byte) error {
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*a = []string{single}
		return nil
	}
	var list []string
	if err := json.Unmarshal(data, &list); err != nil {
		return err
	}
	*a = list
	return nil
}

// Contains reports whether the audience list contains the target audience.
func (a Audience) Contains(target string) bool {
	for _, aud := range a {
		if aud == target {
			return true
		}
	}
	return false
}

// Claims represents the standard OIDC claims extracted from a validated JWT.
type Claims struct {
	Subject           string   `json:"sub"`
	Email             string   `json:"email"`
	PreferredUsername string   `json:"preferred_username"`
	Name              string   `json:"name"`
	Issuer            string   `json:"iss"`
	Audience          Audience `json:"aud"`
	ExpiresAt         int64    `json:"exp"`
	NotBefore         int64    `json:"nbf"`
}

// Validator validates raw JWT tokens against an identity provider.
type Validator interface {
	Validate(ctx context.Context, rawToken string) (*Claims, error)
}

// OIDCValidator validates JWT tokens against an OIDC provider with cached JWKS.
type OIDCValidator struct {
	issuer     string
	audience   string
	httpClient *http.Client

	mu        sync.RWMutex
	jwksURI   string
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time
	cacheTTL  time.Duration
}

// NewOIDCValidator constructs a new OIDCValidator.
func NewOIDCValidator(issuer, audience string, hc *http.Client) *OIDCValidator {
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	return &OIDCValidator{
		issuer:     issuer,
		audience:   audience,
		httpClient: hc,
		keys:       make(map[string]*rsa.PublicKey),
		cacheTTL:   10 * time.Minute,
	}
}

// Init fetches the JWKS immediately, returning an error if it fails.
func (v *OIDCValidator) Init(ctx context.Context) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.refreshKeySetLocked(ctx)
}

// SetCacheTTL adjusts the key cache expiration time (useful in tests).
func (v *OIDCValidator) SetCacheTTL(ttl time.Duration) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.cacheTTL = ttl
}

// Validate parses, verifies, and extracts claims from a raw JWT string.
func (v *OIDCValidator) Validate(ctx context.Context, rawToken string) (*Claims, error) {
	parts := strings.Split(rawToken, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("%w: expected 3 dot-separated parts", ErrInvalidToken)
	}

	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("%w: decoding header: %w", ErrInvalidToken, err)
	}

	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return nil, fmt.Errorf("%w: unmarshaling header: %w", ErrInvalidToken, err)
	}

	if header.Alg == "" || strings.ToLower(header.Alg) == "none" {
		return nil, fmt.Errorf("%w: 'none' algorithm is not allowed", ErrInvalidToken)
	}

	key, err := v.getKey(ctx, header.Kid)
	if err != nil {
		return nil, err
	}

	// Verify signature
	signedContent := []byte(parts[0] + "." + parts[1])
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("%w: decoding signature: %w", ErrInvalidToken, err)
	}

	if err := verifySignature(key, header.Alg, signedContent, sig); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}

	// Unmarshal claims
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: decoding payload: %w", ErrInvalidToken, err)
	}

	var claims Claims
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return nil, fmt.Errorf("%w: unmarshaling payload: %w", ErrInvalidToken, err)
	}

	// Validate claims
	if claims.Issuer != v.issuer {
		return nil, fmt.Errorf("%w: got %q, want %q", ErrIssuerMismatch, claims.Issuer, v.issuer)
	}

	if v.audience == "" || !claims.Audience.Contains(v.audience) {
		return nil, fmt.Errorf("%w: audience %q not found in %+v", ErrAudienceMismatch, v.audience, claims.Audience)
	}

	now := time.Now().Unix()
	if claims.ExpiresAt > 0 && now > claims.ExpiresAt {
		return nil, ErrTokenExpired
	}

	if claims.NotBefore > 0 && now < claims.NotBefore {
		return nil, ErrTokenNotYetValid
	}

	return &claims, nil
}

func verifySignature(pub *rsa.PublicKey, alg string, content, sig []byte) error {
	var (
		h  stdhash.Hash
		ch crypto.Hash
	)

	switch alg {
	case "RS256":
		h = sha256.New()
		ch = crypto.SHA256
	case "RS384":
		h = sha512.New384()
		ch = crypto.SHA384
	case "RS512":
		h = sha512.New()
		ch = crypto.SHA512
	default:
		return fmt.Errorf("unsupported algorithm %q", alg)
	}

	h.Write(content)
	digest := h.Sum(nil)

	return rsa.VerifyPKCS1v15(pub, ch, digest, sig)
}

func (v *OIDCValidator) getKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.RLock()
	key, exists := v.keys[kid]
	fresh := time.Since(v.fetchedAt) < v.cacheTTL
	v.mu.RUnlock()

	if exists && fresh {
		return key, nil
	}

	// Refresh key set
	v.mu.Lock()
	defer v.mu.Unlock()

	// Double check after acquire
	if key, exists := v.keys[kid]; exists && time.Since(v.fetchedAt) < v.cacheTTL {
		return key, nil
	}

	if err := v.refreshKeySetLocked(ctx); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrKeySetUnavailable, err)
	}

	if key, exists := v.keys[kid]; exists {
		return key, nil
	}

	return nil, fmt.Errorf("%w: kid %q", ErrUnknownKey, kid)
}

func (v *OIDCValidator) refreshKeySetLocked(ctx context.Context) error {
	if v.jwksURI == "" {
		discoveryURL := v.issuer + "/.well-known/openid-configuration"
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
		if err != nil {
			return err
		}

		resp, err := v.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("discovery request failed: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("discovery endpoint returned %s", resp.Status)
		}

		var doc struct {
			JwksURI string `json:"jwks_uri"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
			return fmt.Errorf("decoding discovery doc: %w", err)
		}
		if doc.JwksURI == "" {
			return errors.New("discovery document missing jwks_uri")
		}
		v.jwksURI = doc.JwksURI
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURI, nil)
	if err != nil {
		return err
	}

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("jwks request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jwks endpoint returned %s", resp.Status)
	}

	var jwks struct {
		Keys []struct {
			Kty string `json:"kty"`
			Alg string `json:"alg"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return fmt.Errorf("decoding jwks: %w", err)
	}

	newKeys := make(map[string]*rsa.PublicKey)
	for _, k := range jwks.Keys {
		if k.Kty != "RSA" || k.Kid == "" || k.N == "" || k.E == "" {
			continue
		}

		if k.Alg != "" && k.Alg != "RS256" && k.Alg != "RS384" && k.Alg != "RS512" {
			continue
		}

		nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			continue
		}

		eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			continue
		}

		n := new(big.Int).SetBytes(nBytes)
		var eInt int
		for _, b := range eBytes {
			eInt = (eInt << 8) | int(b)
		}

		newKeys[k.Kid] = &rsa.PublicKey{N: n, E: eInt}
	}

	if len(newKeys) == 0 {
		return errors.New("no valid RSA keys found in JWKS")
	}

	v.keys = newKeys
	v.fetchedAt = time.Now()
	return nil
}
