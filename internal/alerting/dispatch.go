package alerting

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// ChannelType represents an external notification channel.
type ChannelType string

const (
	ChannelWebhook   ChannelType = "webhook"
	ChannelSlack     ChannelType = "slack"
	ChannelPagerDuty ChannelType = "pagerduty"
)

// ChannelDestination specifies where an alert notification should be delivered.
type ChannelDestination struct {
	Type       ChannelType `json:"type"`
	URL        string      `json:"url"`
	Secret     string      `json:"secret,omitempty"`
	RoutingKey string      `json:"routing_key,omitempty"`
}

// AlertNotification contains details of an alert to deliver to external channels.
type AlertNotification struct {
	RuleID   string            `json:"rule_id"`
	RuleType string            `json:"rule_type"`
	AppName  string            `json:"app_name"`
	Message  string            `json:"message"`
	Metadata map[string]string `json:"metadata,omitempty"`
	FiredAt  time.Time         `json:"fired_at"`
}

// ChannelDispatcher dispatches alerts to external sinks like webhooks and Slack.
type ChannelDispatcher interface {
	Dispatch(ctx context.Context, dest ChannelDestination, notif AlertNotification) error
}

// HTTPChannelOption configures HTTPChannelDispatcher behavior.
type HTTPChannelOption func(*httpChannelConfig)

type httpChannelConfig struct {
	allowLoopback bool
}

// WithAllowLoopback configures whether loopback destinations are permitted (primarily for test fixtures).
func WithAllowLoopback(allow bool) HTTPChannelOption {
	return func(c *httpChannelConfig) {
		c.allowLoopback = allow
	}
}

// HTTPChannelDispatcher sends alerts over HTTP/HTTPS with retries and signature options.
type HTTPChannelDispatcher struct {
	client *http.Client
}

// isBlockedIP checks if an IP address is private, link-local, loopback, multicast, or unspecified.
func isBlockedIP(ip net.IP, allowLoopback bool) error {
	if ip == nil {
		return fmt.Errorf("invalid IP address")
	}
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}
	if ip.IsLoopback() {
		if allowLoopback {
			return nil
		}
		return fmt.Errorf("access to loopback addresses is blocked: %s", ip)
	}
	if ip.IsPrivate() {
		return fmt.Errorf("access to private IP addresses is blocked: %s", ip)
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("access to link-local addresses is blocked: %s", ip)
	}
	if ip.IsUnspecified() {
		return fmt.Errorf("access to unspecified addresses is blocked: %s", ip)
	}
	if ip.IsMulticast() {
		return fmt.Errorf("access to multicast addresses is blocked: %s", ip)
	}
	return nil
}

// NewSafeHTTPClient returns an http.Client configured with SSRF dialer protection and redirect validation.
func NewSafeHTTPClient(timeout time.Duration, allowLoopback bool) *http.Client {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	dialer := &net.Dialer{
		Timeout:   timeout,
		KeepAlive: 30 * time.Second,
	}

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("parsing target host: %w", err)
			}
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, fmt.Errorf("resolving host %s: %w", host, err)
			}
			if len(ips) == 0 {
				return nil, fmt.Errorf("no IP addresses resolved for host: %s", host)
			}
			for _, ip := range ips {
				if err := isBlockedIP(ip, allowLoopback); err != nil {
					return nil, err
				}
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
		},
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("unsupported redirect scheme: %s", req.URL.Scheme)
			}
			return nil
		},
	}
}

// NewHTTPChannelDispatcher creates an HTTPChannelDispatcher.
func NewHTTPChannelDispatcher(client *http.Client, opts ...HTTPChannelOption) *HTTPChannelDispatcher {
	var cfg httpChannelConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	if client == nil {
		client = NewSafeHTTPClient(10*time.Second, cfg.allowLoopback)
	}
	return &HTTPChannelDispatcher{client: client}
}

// Dispatch routes the notification according to destination channel type.
func (d *HTTPChannelDispatcher) Dispatch(ctx context.Context, dest ChannelDestination, notif AlertNotification) error {
	switch dest.Type {
	case ChannelSlack:
		return d.dispatchSlack(ctx, dest, notif)
	case ChannelPagerDuty:
		return d.dispatchPagerDuty(ctx, dest, notif)
	case ChannelWebhook:
		return d.dispatchWebhook(ctx, dest, notif)
	default:
		return fmt.Errorf("unsupported channel type: %s", dest.Type)
	}
}

func validateDestinationURL(rawURL string) (*url.URL, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parsing destination URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported destination scheme %q: only http and https are permitted", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("destination URL missing host")
	}
	return u, nil
}

func (d *HTTPChannelDispatcher) dispatchWebhook(ctx context.Context, dest ChannelDestination, notif AlertNotification) error {
	if _, err := validateDestinationURL(dest.URL); err != nil {
		return err
	}

	payloadBytes, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("marshaling webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dest.URL, bytes.NewReader(payloadBytes))
	if err != nil {
		return fmt.Errorf("creating webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "relay-alerting/1.0")

	tsStr := strconv.FormatInt(time.Now().Unix(), 10)
	req.Header.Set("X-Relay-Timestamp", tsStr)

	if dest.Secret != "" {
		sig := ComputeWebhookSignature(dest.Secret, tsStr, payloadBytes)
		req.Header.Set("X-Relay-Signature", sig)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("delivering webhook: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook endpoint returned status %d", resp.StatusCode)
	}
	return nil
}

func (d *HTTPChannelDispatcher) dispatchSlack(ctx context.Context, dest ChannelDestination, notif AlertNotification) error {
	if _, err := validateDestinationURL(dest.URL); err != nil {
		return err
	}

	slackBody := map[string]any{
		"text": fmt.Sprintf("⚠️ *[Relay Alert: %s]*: %s (Application: `%s`)", notif.RuleType, notif.Message, notif.AppName),
	}
	payloadBytes, _ := json.Marshal(slackBody)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dest.URL, bytes.NewReader(payloadBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "relay-alerting/1.0")

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("delivering slack alert: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("slack webhook returned status %d", resp.StatusCode)
	}
	return nil
}

func (d *HTTPChannelDispatcher) dispatchPagerDuty(ctx context.Context, dest ChannelDestination, notif AlertNotification) error {
	routingKey := dest.RoutingKey
	if routingKey == "" {
		routingKey = dest.Secret
	}
	pdBody := map[string]any{
		"routing_key":  routingKey,
		"event_action": "trigger",
		"payload": map[string]any{
			"summary":        fmt.Sprintf("%s: %s (%s)", notif.RuleType, notif.Message, notif.AppName),
			"source":         "relay-control-plane",
			"severity":       "error",
			"timestamp":      notif.FiredAt.Format(time.RFC3339),
			"custom_details": notif.Metadata,
		},
	}
	payloadBytes, _ := json.Marshal(pdBody)

	targetURL := dest.URL
	if targetURL == "" {
		targetURL = "https://events.pagerduty.com/v2/enqueue"
	}
	if _, err := validateDestinationURL(targetURL); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "relay-alerting/1.0")

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("delivering pagerduty alert: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("pagerduty endpoint returned status %d", resp.StatusCode)
	}
	return nil
}

// ComputeWebhookSignature computes sha256 HMAC over timestamp.payload.
func ComputeWebhookSignature(secret, timestamp string, payload []byte) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(timestamp + "."))
	h.Write(payload)
	return "sha256=" + hex.EncodeToString(h.Sum(nil))
}

// VerifyWebhookSignature verifies an incoming webhook signature using constant-time comparison.
func VerifyWebhookSignature(secret, timestamp string, payload []byte, signatureHeader string) bool {
	expectedSig := ComputeWebhookSignature(secret, timestamp, payload)
	return hmac.Equal([]byte(signatureHeader), []byte(expectedSig))
}
