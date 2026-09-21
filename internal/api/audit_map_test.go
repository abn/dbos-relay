package api

import (
	"testing"

	"github.com/abn/relay/internal/auth"
)

func TestAuditDeniedOpMapping(t *testing.T) {
	ident := &auth.UserIdentity{Username: "key-name"}
	cases := []struct {
		method, path               string
		op, app, targetT, targetID string
		ok                         bool
	}{
		{"GET", "/v2/orgs/acme/apps/shop/workflows", "", "", "", "", false},
		{"POST", "/v2/orgs/acme/apps/shop/workflows", "", "", "", "", false},
		{"POST", "/v2/users", "", "", "", "", false},
		{"PATCH", "/v2/orgs/acme", "organization.update", "", "organization", "acme", true},
		{"GET", "/v2/orgs/acme", "", "", "", "", false},
		{"POST", "/v2/orgs/acme/join", "user.join", "", "user", "key-name", true},
		{"DELETE", "/v2/orgs/acme/members/bob", "user.remove", "", "user", "bob", true},
		{"PUT", "/v2/orgs/acme/members/bob/roles/admin", "role.grant", "", "user", "bob", true},
		{"POST", "/v2/orgs/acme/roles", "role.create", "", "role", "", true},
		{"DELETE", "/v2/orgs/acme/roles/auditor", "role.delete", "", "role", "auditor", true},
		{"POST", "/v2/orgs/acme/tokens/deploy", "token.create", "", "token", "deploy", true},
		{"DELETE", "/v2/orgs/acme/tokens/deploy", "token.revoke", "", "token", "deploy", true},
		{"PUT", "/v2/orgs/acme/apps/shop", "application.create", "shop", "application", "shop", true},
		{"PATCH", "/v2/orgs/acme/apps/shop", "application.update", "shop", "application", "shop", true},
		{"DELETE", "/v2/orgs/acme/apps/shop", "application.delete", "shop", "application", "shop", true},
		{"PATCH", "/v2/orgs/acme/apps/shop/versions/latest", "application.set_latest_version", "shop", "application", "shop", true},
		{"POST", "/v2/orgs/acme/apps/shop/workflows/abc/cancel", "workflow.cancel", "shop", "workflow", "abc", true},
		{"POST", "/v2/orgs/acme/apps/shop/workflows/bulk-cancel", "workflow.bulk_cancel", "shop", "", "", true},
		{"POST", "/v2/orgs/acme/apps/shop/workflows/abc/resume", "workflow.resume", "shop", "workflow", "abc", true},
		{"POST", "/v2/orgs/acme/apps/shop/workflows/bulk-resume", "workflow.bulk_resume", "shop", "", "", true},
		{"DELETE", "/v2/orgs/acme/apps/shop/workflows/abc", "workflow.delete", "shop", "workflow", "abc", true},
		{"POST", "/v2/orgs/acme/apps/shop/workflows/bulk-delete", "workflow.bulk_delete", "shop", "", "", true},
		{"POST", "/v2/orgs/acme/apps/shop/workflows/abc/fork", "workflow.fork", "shop", "workflow", "abc", true},
		{"POST", "/v2/orgs/acme/apps/shop/workflows/bulk-fork-from-failure", "workflow.fork_from_failure", "shop", "", "", true},
		{"POST", "/v2/orgs/acme/apps/shop/workflows/import", "workflow.import", "shop", "", "", true},
		{"POST", "/v2/orgs/acme/apps/shop/schedules/nightly/pause", "schedule.pause", "shop", "schedule", "nightly", true},
		{"POST", "/v2/orgs/acme/apps/shop/schedules/nightly/resume", "schedule.resume", "shop", "schedule", "nightly", true},
		{"POST", "/v2/orgs/acme/apps/shop/schedules/nightly/trigger", "schedule.trigger", "shop", "schedule", "nightly", true},
		{"POST", "/v2/orgs/acme/apps/shop/schedules/nightly/backfill", "schedule.backfill", "shop", "schedule", "nightly", true},
		{"POST", "/v2/orgs/acme/apps/shop/alerting-rules", "alerting_rule.create", "shop", "alerting_rule", "", true},
		{"DELETE", "/v2/orgs/acme/apps/shop/alerting-rules/rule-1", "alerting_rule.delete", "shop", "alerting_rule", "rule-1", true},
		{"PUT", "/v2/orgs/acme/apps/shop/autoscaling-policy", "autoscaling_policy.set", "shop", "application", "shop", true},
		{"DELETE", "/v2/orgs/acme/apps/shop/autoscaling-policy", "autoscaling_policy.delete", "shop", "application", "shop", true},
		{"POST", "/v2/orgs/acme/secrets", "secret.generate", "", "organization", "acme", true},
		{"GET", "/v2/orgs/acme/apps/shop/autoscaling-policy", "", "", "", "", false},
		{"GET", "/v2/orgs/acme/apps/shop/autoscale", "", "", "", "", false},
		{"GET", "/v2/orgs/acme/apps/shop/queues", "", "", "", "", false},
		{"GET", "/v1/metrics", "", "", "", "", false},
	}
	for _, c := range cases {
		op, app, targetT, targetID, ok := auditDeniedOp(c.method, c.path, ident)
		if ok != c.ok || op != c.op || app != c.app || targetT != c.targetT || targetID != c.targetID {
			t.Errorf("%s %s = (%q,%q,%q,%q,%v), want (%q,%q,%q,%q,%v)",
				c.method, c.path, op, app, targetT, targetID, ok,
				c.op, c.app, c.targetT, c.targetID, c.ok)
		}
	}
}
