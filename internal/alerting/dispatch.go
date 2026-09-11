package alerting

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
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
	RuleID      string            `json:"rule_id"`
	RuleType    string            `json:"rule_type"`
	AppName     string            `json:"app_name"`
	Message     string            `json:"message"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	FiredAt     time.Time         `json:"fired_at"`
}

// ChannelDispatcher dispatches alerts to external sinks like webhooks and Slack.
type ChannelDispatcher interface {
	Dispatch(ctx context.Context, dest ChannelDestination, notif AlertNotification) error
}

// HTTPChannelDispatcher sends alerts over HTTP/HTTPS with retries and signature options.
type HTTPChannelDispatcher struct {
	client *http.Client
}

// NewHTTPChannelDispatcher creates an HTTPChannelDispatcher.
func NewHTTPChannelDispatcher(client *http.Client) *HTTPChannelDispatcher {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
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

func (d *HTTPChannelDispatcher) dispatchWebhook(ctx context.Context, dest ChannelDestination, notif AlertNotification) error {
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
