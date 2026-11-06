package doctor

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/abn/relay/internal/declarative"
)

func runProbeChecks(ctx context.Context, report *Report, manifest *declarative.Config, opts Options) {
	directApps := directApplications(manifest)
	secret := strings.TrimSpace(opts.Env("RELAY_INTERNAL_SECRET"))
	authEnabled := strings.TrimSpace(opts.Env("RELAY_OIDC_ISSUER")) != ""

	switch {
	case secret != "":
		report.Add("probe.secret", StatusPass, "RELAY_INTERNAL_SECRET is set; the value is not displayed.", "")
	case authEnabled:
		report.Add(
			"probe.secret",
			StatusFail,
			"RELAY_INTERNAL_SECRET is empty while authentication is enabled.",
			"Set RELAY_INTERNAL_SECRET; 'relay serve' refuses to start without it when OIDC is configured.",
		)
	case len(directApps) > 0:
		report.Add(
			"probe.secret",
			StatusWarn,
			"RELAY_INTERNAL_SECRET is empty, so the probe endpoint is not mounted.",
			"Set RELAY_INTERNAL_SECRET and run 'relay probe' beside each Direct application to enable automatic recovery.",
		)
	default:
		report.Add("probe.secret", StatusPass, "not required: authentication is disabled and no Direct mode application is configured.", "")
	}

	checkProbeEndpoint(ctx, report, probeURL(opts), opts)
	checkProbeIdentity(report, directApps, opts)
}

func directApplications(manifest *declarative.Config) map[string]string {
	apps := make(map[string]string)
	if manifest == nil {
		return apps
	}
	for name, dp := range manifest.DataPlanes {
		if dp.Integration != "direct" {
			continue
		}
		mode := dp.Mode
		if mode == "" {
			mode = "read"
		}
		apps[name] = mode
	}
	return apps
}

func probeURL(opts Options) string {
	for _, value := range []string{
		opts.RelayURL,
		opts.Env("RELAY_PROBE_URL"),
		opts.Env("RELAY_URL"),
	} {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return "http://127.0.0.1:8090"
}

func checkProbeEndpoint(ctx context.Context, report *Report, rawURL string, opts Options) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		report.Add(
			"probe.endpoint",
			StatusFail,
			"the probe endpoint is not a valid URL.",
			"Pass --relay-url with an http or https URL.",
		)
		return
	}

	switch u.Scheme {
	case "http", "https":
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	default:
		report.Add(
			"probe.endpoint",
			StatusFail,
			fmt.Sprintf("the probe endpoint uses unsupported scheme %q.", u.Scheme),
			"Use http, https, ws, or wss so the probe can reach Relay.",
		)
		return
	}

	target := strings.TrimRight(u.String(), "/") + "/healthz"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		report.Add(
			"probe.endpoint",
			StatusFail,
			"could not build the health request: "+SanitizeError(err),
			"Pass --relay-url with a valid base URL.",
		)
		return
	}

	resp, err := opts.HTTPClient.Do(req)
	if err != nil {
		report.Add(
			"probe.endpoint",
			StatusWarn,
			"GET /healthz from this host failed: "+SanitizeError(err),
			"If doctor is not running on the Relay host, pass --relay-url with the address reachable from the probe hosts.",
		)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode >= http.StatusInternalServerError:
		report.Add(
			"probe.endpoint",
			StatusWarn,
			fmt.Sprintf("GET /healthz returned %s.", resp.Status),
			"Check the Relay logs and whether the server has finished starting.",
		)
	case resp.StatusCode == http.StatusNotFound:
		report.Add(
			"probe.endpoint",
			StatusWarn,
			"GET /healthz returned 404; is the base URL correct?",
			"Pass the Relay base URL without a path prefix.",
		)
	default:
		report.Add(
			"probe.endpoint",
			StatusPass,
			fmt.Sprintf("GET /healthz reached Relay and returned %s.", resp.Status),
			"",
		)
	}
}

func checkProbeIdentity(report *Report, directApps map[string]string, opts Options) {
	if len(directApps) == 0 {
		report.Add("probe.identity", StatusSkip, "no Direct mode application is configured.", "")
		return
	}

	app := strings.TrimSpace(opts.ProbeApp)
	if app == "" {
		app = strings.TrimSpace(opts.Env("DBOS_APP_NAME"))
	}
	if app == "" && len(directApps) == 1 {
		for name := range directApps {
			app = name
		}
	}

	if app == "" {
		report.Add(
			"probe.identity",
			StatusWarn,
			"probe application identity is not set on this host.",
			"Set DBOS_APP_NAME or --probe-app to the Direct application name, then rerun doctor on the application host.",
		)
		return
	}

	mode, ok := directApps[app]
	if !ok {
		report.Add(
			"probe.identity",
			StatusFail,
			fmt.Sprintf("application %q is not configured with integration: direct.", app),
			"The probe endpoint accepts only Direct mode applications. Match the manifest application name, or remove the probe from this host.",
		)
		return
	}

	executor := strings.TrimSpace(opts.ProbeExecutorID)
	if executor == "" {
		executor = strings.TrimSpace(opts.Env("DBOS__VMID"))
	}
	if executor == "" {
		report.Add(
			"probe.identity",
			StatusWarn,
			fmt.Sprintf("application %q has no executor identity on this host.", app),
			"Set DBOS__VMID or --probe-executor-id to the executor ID the SDK uses.",
		)
		return
	}

	if mode != "read-write" {
		report.Add(
			"probe.identity",
			StatusWarn,
			fmt.Sprintf("application %q uses mode %q, so a dead executor cannot be recovered through the database.", app, mode),
			"Set mode: read-write for Direct applications that need automatic recovery.",
		)
		return
	}

	report.Add(
		"probe.identity",
		StatusPass,
		fmt.Sprintf("probe identity is app=%q executor=%q and the application is read-write.", app, executor),
		"",
	)
}
