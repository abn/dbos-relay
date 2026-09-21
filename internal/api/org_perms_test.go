package api

import (
	"net/http"
	"testing"

	"github.com/abn/relay/internal/auth"
)

func TestOrgLevelRequiredPerm(t *testing.T) {
	cases := []struct {
		method, path string
		want         string
	}{
		{"GET", "/v2/orgs/acme", auth.PermOrgRead},
		{"PATCH", "/v2/orgs/acme", auth.PermOrgWrite},
		{"GET", "/v2/orgs/acme/apps", auth.PermApplicationRead},
		{"PUT", "/v2/orgs/acme/apps/shop", auth.PermApplicationWrite},
		{"GET", "/v2/orgs/acme/tokens", auth.PermTokenRead},
		{"POST", "/v2/orgs/acme/tokens/deploy", auth.PermTokenWrite},
		{"GET", "/v2/orgs/acme/permissions", ""},
		{"POST", "/v2/orgs/acme/join", ""},
		{"POST", "/v2/orgs/acme/secrets", ""},
		{"GET", "/v2/orgs/acme/members", auth.PermOrgRead},
		{"DELETE", "/v2/orgs/acme/members/bob", auth.PermOrgWrite},
		{"GET", "/v2/orgs/acme/audit-logs", auth.PermOrgRead},
		{http.MethodGet, "/v2/orgs/acme/permissions/", ""},
	}
	for _, c := range cases {
		if got := orgLevelRequiredPerm(c.method, c.path); got != c.want {
			t.Errorf("%s %s = %q, want %q", c.method, c.path, got, c.want)
		}
	}
}

func TestHasMetricsSuffix(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/v2/orgs/acme/apps/shop/metrics", true},
		{"/v2/orgs/acme/apps/shop/metrics/", true},
		{"/v2/orgs/acme/apps/shop/queues", false},
		{"/v1/metrics", false},
		{"/v2/orgs/acme/apps/shop/workflows/metrics", false},
		{"/v2/orgs/acme/apps/shop/bar/metrics", false},
		{"/v2/orgs/acme/metrics", false},
	}
	for _, c := range cases {
		if got := hasMetricsSuffix(c.path); got != c.want {
			t.Errorf("%s = %v, want %v", c.path, got, c.want)
		}
	}
}
