package doctor

import (
	"context"
	"testing"

	"github.com/abn/relay/internal/declarative"
)

func directManifest(mode string) *declarative.Config {
	return &declarative.Config{
		DataPlanes: map[string]declarative.DataPlane{
			"orders": {Integration: "direct", Mode: mode},
		},
	}
}

func TestProbeChecks_SecretRequiredWithAuth(t *testing.T) {
	report := &Report{}
	runProbeChecks(context.Background(), report, directManifest("read-write"), Options{
		RelayURL: healthServer(t).URL,
		Env: env(map[string]string{
			"RELAY_OIDC_ISSUER": "https://auth.example.com",
		}),
		HTTPClient: healthServer(t).Client(),
	})
	if got := findCheck(t, report, "probe.secret"); got.Status != StatusFail {
		t.Fatalf("probe.secret = %s, want FAIL", got.Status)
	}
}

func TestProbeChecks_SecretOptionalWithoutDirect(t *testing.T) {
	report := &Report{}
	runProbeChecks(context.Background(), report, &declarative.Config{}, Options{
		RelayURL:   healthServer(t).URL,
		Env:        env(nil),
		HTTPClient: healthServer(t).Client(),
	})
	if got := findCheck(t, report, "probe.secret"); got.Status != StatusPass {
		t.Fatalf("probe.secret = %s, want PASS", got.Status)
	}
	if got := findCheck(t, report, "probe.identity"); got.Status != StatusSkip {
		t.Fatalf("probe.identity = %s, want SKIP", got.Status)
	}
}

func TestProbeChecks_DirectWithoutSecretWarns(t *testing.T) {
	report := &Report{}
	runProbeChecks(context.Background(), report, directManifest("read-write"), Options{
		RelayURL:   healthServer(t).URL,
		Env:        env(nil),
		HTTPClient: healthServer(t).Client(),
	})
	if got := findCheck(t, report, "probe.secret"); got.Status != StatusWarn {
		t.Fatalf("probe.secret = %s, want WARN", got.Status)
	}
}

func TestProbeChecks_IdentityPass(t *testing.T) {
	server := healthServer(t)
	report := &Report{}
	runProbeChecks(context.Background(), report, directManifest("read-write"), Options{
		RelayURL: server.URL,
		Env: env(map[string]string{
			"RELAY_INTERNAL_SECRET": "secret",
			"DBOS_APP_NAME":         "orders",
			"DBOS__VMID":            "orders-0",
		}),
		HTTPClient: server.Client(),
	})
	if got := findCheck(t, report, "probe.identity"); got.Status != StatusPass {
		t.Fatalf("probe.identity = %s, want PASS (detail: %s)", got.Status, got.Detail)
	}
}

func TestProbeChecks_IdentityNotDirectFails(t *testing.T) {
	server := healthServer(t)
	report := &Report{}
	runProbeChecks(context.Background(), report, directManifest("read-write"), Options{
		RelayURL: server.URL,
		ProbeApp: "billing",
		Env: env(map[string]string{
			"RELAY_INTERNAL_SECRET": "secret",
			"DBOS__VMID":            "billing-0",
		}),
		HTTPClient: server.Client(),
	})
	if got := findCheck(t, report, "probe.identity"); got.Status != StatusFail {
		t.Fatalf("probe.identity = %s, want FAIL", got.Status)
	}
}

func TestProbeChecks_ReadModeWarns(t *testing.T) {
	server := healthServer(t)
	report := &Report{}
	runProbeChecks(context.Background(), report, directManifest("read"), Options{
		RelayURL: server.URL,
		Env: env(map[string]string{
			"RELAY_INTERNAL_SECRET": "secret",
			"DBOS_APP_NAME":         "orders",
			"DBOS__VMID":            "orders-0",
		}),
		HTTPClient: server.Client(),
	})
	if got := findCheck(t, report, "probe.identity"); got.Status != StatusWarn {
		t.Fatalf("probe.identity = %s, want WARN", got.Status)
	}
}
