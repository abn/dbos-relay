package router

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/protocol"
)

const (
	// HeaderSignature is the HMAC signature header.
	HeaderSignature = "X-Relay-Forward-Signature"
	// HeaderTimestamp is the Unix timestamp header.
	HeaderTimestamp = "X-Relay-Forward-Timestamp"
	// HeaderHop is the integer hop counter header.
	HeaderHop = "X-Relay-Forward-Hop"
	// HeaderDeadline is the context deadline header formatted in RFC 3339.
	HeaderDeadline = "X-Relay-Forward-Deadline"
	// HeaderNonce is the replay-protection nonce header.
	HeaderNonce = "X-Relay-Forward-Nonce"
)

var (
	ErrInvalidSignature = errors.New("invalid forward signature")
	ErrExpiredTimestamp = errors.New("expired forward timestamp")
	ErrLoopDetected     = errors.New("forward loop detected")
	ErrMissingHeaders   = errors.New("missing forward authentication headers")
	ErrExpiredDeadline  = errors.New("expired forward deadline")
	ErrReplayedNonce    = errors.New("replayed forward nonce")
)

type nonceCache struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

func newNonceCache() *nonceCache {
	return &nonceCache{seen: make(map[string]time.Time)}
}

func (c *nonceCache) checkAndRecord(nonce string, now time.Time, ttl time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	for k, t := range c.seen {
		if now.Sub(t) > ttl {
			delete(c.seen, k)
		}
	}

	if _, exists := c.seen[nonce]; exists {
		return false
	}
	c.seen[nonce] = now
	return true
}

// SignRequest calculates and attaches HMAC authentication headers to an outgoing forward request.
func SignRequest(req *http.Request, body []byte, secret []byte, hop int, deadline time.Time) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	hopStr := strconv.Itoa(hop)

	nonce := req.Header.Get(HeaderNonce)
	if nonce == "" {
		raw := make([]byte, 16)
		_, _ = rand.Read(raw)
		nonce = hex.EncodeToString(raw)
		req.Header.Set(HeaderNonce, nonce)
	}

	req.Header.Set(HeaderTimestamp, ts)
	req.Header.Set(HeaderHop, hopStr)
	deadlineStr := ""
	if !deadline.IsZero() {
		deadlineStr = deadline.Format(time.RFC3339Nano)
		req.Header.Set(HeaderDeadline, deadlineStr)
	}

	payload := buildSignaturePayload(ts, hopStr, req.Method, req.URL.RequestURI(), nonce, deadlineStr, body)
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	req.Header.Set(HeaderSignature, hex.EncodeToString(mac.Sum(nil)))
}

// VerifyRequest verifies the HMAC signature, timestamp drift, and hop counter of an incoming forward request.
func VerifyRequest(req *http.Request, body []byte, secret []byte, maxDrift time.Duration) (int, time.Time, error) {
	if len(secret) == 0 {
		return 0, time.Time{}, ErrInvalidSignature
	}

	sig := req.Header.Get(HeaderSignature)
	tsStr := req.Header.Get(HeaderTimestamp)
	hopStr := req.Header.Get(HeaderHop)
	nonce := req.Header.Get(HeaderNonce)
	deadlineStr := req.Header.Get(HeaderDeadline)

	if sig == "" || tsStr == "" || hopStr == "" || nonce == "" {
		return 0, time.Time{}, ErrMissingHeaders
	}

	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("invalid timestamp header: %w", err)
	}

	now := time.Now()
	drift := time.Duration(abs(now.Unix()-ts)) * time.Second
	if drift > maxDrift {
		return 0, time.Time{}, ErrExpiredTimestamp
	}

	hop, err := strconv.Atoi(hopStr)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("invalid hop header: %w", err)
	}
	if hop >= 1 {
		return hop, time.Time{}, ErrLoopDetected
	}

	var deadline time.Time
	if deadlineStr != "" {
		parsed, err := time.Parse(time.RFC3339Nano, deadlineStr)
		if err != nil {
			return 0, time.Time{}, fmt.Errorf("invalid deadline header: %w", err)
		}
		if !parsed.IsZero() {
			senderStart := time.Unix(ts, int64(parsed.Nanosecond()))
			budget := parsed.Sub(senderStart)
			if budget <= 0 {
				return 0, time.Time{}, ErrExpiredDeadline
			}
			const minBudget = 500 * time.Millisecond
			const maxBudget = 60 * time.Second
			if budget < minBudget {
				budget = minBudget
			}
			if budget > maxBudget {
				budget = maxBudget
			}
			if abs(now.Unix()-ts) == 0 && parsed.After(now) && parsed.Sub(senderStart) <= maxBudget {
				deadline = parsed
			} else {
				deadline = now.Add(budget)
			}
		}
	}

	payload := buildSignaturePayload(tsStr, hopStr, req.Method, req.URL.RequestURI(), nonce, deadlineStr, body)
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	if subtle.ConstantTimeCompare([]byte(sig), []byte(expectedSig)) != 1 {
		return 0, time.Time{}, ErrInvalidSignature
	}

	return hop, deadline, nil
}

func buildSignaturePayload(ts, hop, method, uri, nonce, deadline string, body []byte) []byte {
	return []byte(fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s\n%s", ts, hop, method, uri, nonce, deadline, string(body)))
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// Forwarder dispatches requests to peer Relay instances over HTTP.
type Forwarder struct {
	secret []byte
	client *http.Client
}

// NewForwarder creates a new peer Forwarder.
func NewForwarder(secret []byte, client *http.Client) *Forwarder {
	if client == nil {
		client = &http.Client{
			Timeout: 30 * time.Second,
		}
	}
	return &Forwarder{
		secret: secret,
		client: client,
	}
}

// Forward transmits a protocol message to a target peer URL and returns the decoded protocol response.
func (f *Forwarder) Forward(ctx context.Context, targetURL string, msg protocol.Message) (protocol.Message, error) {
	body, err := protocol.Encode(msg)
	if err != nil {
		return nil, fmt.Errorf("encoding protocol message for forward: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating forward request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(30 * time.Second)
	}

	SignRequest(req, body, f.secret, 0, deadline)

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dispatching forward request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading forward response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("forward peer returned status %d: %s", resp.StatusCode, string(respBody))
	}

	decoded, err := protocol.DecodeResponse(respBody)
	if err != nil {
		return nil, fmt.Errorf("decoding forward response: %w", err)
	}

	return decoded, nil
}

// NewForwardHandler creates an HTTP handler for processing incoming peer forward requests.
func NewForwardHandler(dispatcher Dispatcher, secret []byte, maxDrift time.Duration) http.Handler {
	if maxDrift <= 0 {
		maxDrift = 30 * time.Second
	}
	cache := newNonceCache()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if len(secret) == 0 {
			http.Error(w, "peer forwarding not configured", http.StatusUnauthorized)
			return
		}

		path := strings.Trim(r.URL.Path, "/")
		parts := strings.Split(path, "/")
		if len(parts) < 4 || parts[0] != "internal" || parts[1] != "v1" || parts[2] != "forward" {
			http.Error(w, "invalid forward path", http.StatusBadRequest)
			return
		}

		var appID pgtype.UUID
		if err := appID.Scan(parts[3]); err != nil || !appID.Valid {
			http.Error(w, "invalid application id", http.StatusBadRequest)
			return
		}

		// Cheap header rejections before reading body
		sig := r.Header.Get(HeaderSignature)
		tsStr := r.Header.Get(HeaderTimestamp)
		hopStr := r.Header.Get(HeaderHop)
		nonce := r.Header.Get(HeaderNonce)
		deadlineStr := r.Header.Get(HeaderDeadline)

		if sig == "" || tsStr == "" || hopStr == "" || nonce == "" {
			http.Error(w, ErrMissingHeaders.Error(), http.StatusUnauthorized)
			return
		}

		ts, err := strconv.ParseInt(tsStr, 10, 64)
		if err != nil {
			http.Error(w, "invalid timestamp header", http.StatusUnauthorized)
			return
		}

		now := time.Now()
		drift := time.Duration(abs(now.Unix()-ts)) * time.Second
		if drift > maxDrift {
			http.Error(w, ErrExpiredTimestamp.Error(), http.StatusUnauthorized)
			return
		}

		hop, err := strconv.Atoi(hopStr)
		if err != nil {
			http.Error(w, "invalid hop header", http.StatusUnauthorized)
			return
		}
		if hop >= 1 {
			http.Error(w, ErrLoopDetected.Error(), http.StatusConflict)
			return
		}

		if deadlineStr != "" {
			dl, err := time.Parse(time.RFC3339Nano, deadlineStr)
			if err != nil {
				http.Error(w, "invalid deadline header", http.StatusUnauthorized)
				return
			}
			if !dl.IsZero() && dl.Sub(time.Unix(ts, 0)) <= 0 {
				http.Error(w, ErrExpiredDeadline.Error(), http.StatusUnauthorized)
				return
			}
		}

		// Replay check using nonce cache
		if !cache.checkAndRecord(nonce, now, maxDrift) {
			http.Error(w, ErrReplayedNonce.Error(), http.StatusUnauthorized)
			return
		}

		// Limit body read to 10 MiB with http.MaxBytesReader
		const maxForwardBodyBytes = 10 * 1024 * 1024
		r.Body = http.MaxBytesReader(w, r.Body, maxForwardBodyBytes)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}

		_, deadline, err := VerifyRequest(r, body, secret, maxDrift)
		if err != nil {
			if errors.Is(err, ErrLoopDetected) {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		msg, err := protocol.Decode(body)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to decode message: %v", err), http.StatusBadRequest)
			return
		}

		ctx := r.Context()
		if !deadline.IsZero() {
			var cancel context.CancelFunc
			ctx, cancel = context.WithDeadline(ctx, deadline)
			defer cancel()
		}

		res, err := dispatcher.Dispatch(ctx, appID, msg)
		if err != nil {
			http.Error(w, fmt.Sprintf("dispatch error: %v", err), http.StatusBadGateway)
			return
		}

		resBytes, err := protocol.Encode(res)
		if err != nil {
			http.Error(w, fmt.Sprintf("encoding response: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(resBytes)
	})
}
