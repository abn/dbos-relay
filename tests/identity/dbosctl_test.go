package identity_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/abn/relay/internal/auth"
)

// This test drives the real upstream dbosctl binary (not a stub) through
// the OIDC device flow against Relay with the in-test mock identity
// provider standing in for an external OIDC provider.
//
// The binary is resolved via PATH. Install it with:
// go install github.com/dbos-inc/dbos-ctl/cmd/dbosctl@latest
func TestIdentity_DbosctlDeviceLogin(t *testing.T) {
	t.Logf("identity provider: in-test mockIdP stand-in (not a real external OIDC provider)")
	t.Logf("client: real dbosctl binary (not a stub)")

	bin, err := exec.LookPath("dbosctl")
	if err != nil {
		t.Skip("skipping: dbosctl binary not found in PATH (install with: go install github.com/dbos-inc/dbos-ctl/cmd/dbosctl@latest)")
	}

	idp := newDeviceAuthServer(t)
	store := newMemoryStore()

	validator := auth.NewOIDCValidator(idp.server.URL, "relay-client", idp.server.Client())
	ts := setupServer(store, true, validator)
	defer ts.Close()

	// Isolate dbosctl profiles and credentials from the developer machine.
	// HOME is redirected as well as XDG_CONFIG_HOME because config
	// resolution ignores XDG variables on some platforms.
	xdg := t.TempDir()
	env := append(os.Environ(), "XDG_CONFIG_HOME="+xdg, "HOME="+xdg, "BROWSER=true")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	run := func(args ...string) (string, int) {
		t.Helper()
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		code := 0
		if err != nil {
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				code = exit.ExitCode()
			} else {
				t.Fatalf("run %v: %v", args, err)
			}
		}
		return string(out), code
	}

	if out, code := run("config", "set", "relaytest",
		"--url", ts.URL,
		"--issuer", idp.server.URL,
		"--client-id", "relay-client",
	); code != 0 {
		t.Fatalf("config set failed (%d): %s", code, out)
	}

	// Start the device login; it blocks polling until the IdP approval
	// lands, so approve from this goroutine once the user code appears.
	type loginResult struct {
		out  string
		code int
	}
	done := make(chan loginResult, 1)
	go func() {
		out, code := run("login", "--profile", "relaytest")
		done <- loginResult{out, code}
	}()

	userCode := ""
	var userRec *deviceCodeRecord
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		idp.mu.Lock()
		for code, rec := range idp.codesByUser {
			userCode = code
			userRec = rec
		}
		idp.mu.Unlock()
		if userCode != "" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if userCode == "" || userRec == nil {
		t.Fatal("dbosctl login never requested a device code")
	}
	idp.approveDeviceCode(userRec.deviceCode, map[string]any{
		"sub":                "dbosctl-user-sub",
		"email":              "dbosctl@example.com",
		"preferred_username": "dbosctl-user",
	})

	var res loginResult
	select {
	case res = <-done:
	case <-time.After(90 * time.Second):
		t.Fatal("dbosctl login did not complete after approval")
	}
	if res.code != 0 {
		t.Fatalf("login failed (%d): %s", res.code, res.out)
	}

	if out, code := run("whoami", "--profile", "relaytest"); code != 0 {
		t.Fatalf("whoami failed (%d): %s", code, out)
	} else if !strings.Contains(out, "dbosctl-user") {
		t.Errorf("whoami output missing test user, got:\n%s", out)
	}

	if out, code := run("app", "list", "--profile", "relaytest"); code != 0 {
		t.Fatalf("app list failed (%d): %s", code, out)
	}

	if out, code := run("app", "register", "dbosctl-app", "--profile", "relaytest"); code != 0 {
		t.Fatalf("app register failed (%d): %s", code, out)
	} else if !strings.Contains(out, "dbosctl-app") {
		t.Errorf("app register output missing app name, got:\n%s", out)
	}

	if out, code := run("app", "list", "--profile", "relaytest"); code != 0 {
		t.Fatalf("app list failed (%d): %s", code, out)
	} else if !strings.Contains(out, "dbosctl-app") {
		t.Errorf("app list missing registered app, got:\n%s", out)
	}

	// The stored profile must resolve the personal org for later runs.
	cfgData, err := os.ReadFile(filepath.Join(xdg, "dbos", "config.yaml"))
	if err != nil {
		t.Fatalf("read dbosctl config: %v", err)
	}
	if matched, _ := regexp.MatchString(`(?m)^\s*org:`, string(cfgData)); !matched {
		t.Logf("config has no pinned org (derived per login); config:\n%s", cfgData)
	}
}
