package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/api/gen"
	"github.com/abn/relay/internal/auth"
	"github.com/abn/relay/internal/safego"
	storegen "github.com/abn/relay/internal/store/gen"
)

// Audit operation names follow the Conductor audit taxonomy
// (https://docs.dbos.dev/production/audit-logs, confirmed 2026-09-08).
const (
	auditOpAppCreate          = "application.create"
	auditOpAppUpdate          = "application.update"
	auditOpAppDelete          = "application.delete"
	auditOpAppSetLatest       = "application.set_latest_version"
	auditOpWorkflowCancel     = "workflow.cancel"
	auditOpWorkflowResume     = "workflow.resume"
	auditOpWorkflowFork       = "workflow.fork"
	auditOpWorkflowForkFail   = "workflow.fork_from_failure"
	auditOpWorkflowDelete     = "workflow.delete"
	auditOpWorkflowImport     = "workflow.import"
	auditOpWorkflowBulkCancel = "workflow.bulk_cancel"
	auditOpWorkflowBulkDelete = "workflow.bulk_delete"
	auditOpWorkflowBulkResume = "workflow.bulk_resume"
	auditOpSchedulePause      = "schedule.pause"
	auditOpScheduleResume     = "schedule.resume"
	auditOpScheduleTrigger    = "schedule.trigger"
	auditOpScheduleBackfill   = "schedule.backfill"
	auditOpAlertRuleCreate    = "alerting_rule.create"
	auditOpAlertRuleDelete    = "alerting_rule.delete"
	auditOpTokenCreate        = "token.create"
	auditOpTokenRevoke        = "token.revoke"
	auditOpRoleCreate         = "role.create"
	auditOpRoleDelete         = "role.delete"
	auditOpRoleGrant          = "role.grant"
	auditOpOrgUpdate          = "organization.update"
	auditOpUserJoin           = "user.join"
	auditOpUserRemove         = "user.remove"
	// Join-secret generation has no upstream taxonomy operation either.
	// It is recorded as a Relay extension like domain claims.
	auditOpSecretGenerate = "secret.generate"
	// Domain-claim mutations have no upstream taxonomy operation and are
	// recorded as Relay extensions.
	auditOpDomainClaimCreate = "domain_claim.create"
	auditOpDomainClaimDelete = "domain_claim.delete"
)

const (
	auditStatusSuccess = "success"
	auditStatusFailure = "failure"
)

// DefaultAuditLogRetentionDays matches the upstream default retention
// window for audit entries. Valid range: 7 to 3650 days.
const (
	DefaultAuditLogRetentionDays = 90
	MinAuditLogRetentionDays     = 7
	MaxAuditLogRetentionDays     = 3650
)

// auditSweepInterval is how often expired audit entries are purged.
const auditSweepInterval = time.Hour

type auditIPKey struct{}

// StashSourceIP stores the caller IP in ctx for audit recording.
func StashSourceIP(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, auditIPKey{}, ip)
}

// SourceIPFromContext returns the stashed caller IP, if any.
func SourceIPFromContext(ctx context.Context) string {
	if ip, ok := ctx.Value(auditIPKey{}).(string); ok && ip != "" {
		return ip
	}
	return ""
}

func getRealIPFromHeaders(xff, remoteAddr string) string {
	if xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	return remoteAddr
}

// auditOperation records a structured audit entry for a mutating operation.
// The details envelope carries subject and target identity plus
// operation-specific context; subject display names are preserved in the
// entry so they survive later deletion of the user or key. Entries are
// scoped to the organisation and silently skipped when it cannot be
// resolved or the store is unavailable.
func (s *Server) auditOperation(ctx context.Context, orgName, appName, operation, status, targetType, targetID string, fields map[string]any) {
	ident, _ := auth.IdentityFromContext(ctx)
	s.auditOperationAs(ctx, ident, orgName, appName, operation, status, targetType, targetID, fields)
}

func (s *Server) auditOperationAs(ctx context.Context, ident *auth.UserIdentity, orgName, appName, operation, status, targetType, targetID string, fields map[string]any) {
	if s.store == nil {
		return
	}
	org, err := s.store.GetOrganisationByName(ctx, normalizeOrg(orgName))
	if err != nil {
		return
	}

	subjectType := "user"
	subjectID := ""
	display := "system"
	if ident != nil {
		if ident.IsAPIKey {
			subjectType = "api_key"
			subjectID = ident.Subject
			display = ident.Username
		} else {
			subjectID = ident.Subject
			display = ident.Email
			if display == "" {
				display = ident.Username
			}
		}
	}
	if display == "" {
		display = "system"
	}

	details := make(map[string]any, len(fields)+8)
	for k, v := range fields {
		details[k] = v
	}
	details["status"] = status
	details["subject_type"] = subjectType
	details["subject_id"] = subjectID
	details["subject_display"] = display
	if targetType != "" {
		details["target_type"] = targetType
		details["target_id"] = targetID
	}
	if ip := SourceIPFromContext(ctx); ip != "" {
		details["source_ip"] = ip
	}
	if appName != "" {
		details["application_name"] = appName
	}
	detBytes, _ := json.Marshal(details)
	if len(detBytes) == 0 {
		detBytes = []byte("{}")
	}

	_, _ = s.store.CreateAuditLog(ctx, storegen.CreateAuditLogParams{
		OrganisationID: org.ID,
		Username:       display,
		Action:         operation,
		Details:        detBytes,
	})
}

// domainForAudit extracts the requested domain for audit targets,
// tolerating a missing body on rejected requests.
func domainForAudit(body *gen.RequestDomainClaimJSONRequestBody) string {
	if body != nil {
		return body.Domain
	}
	return ""
}

// roleNameForAudit extracts the requested role name for audit targets,
// tolerating a missing body on rejected requests.
func roleNameForAudit(body *gen.CreateRoleJSONRequestBody) string {
	if body != nil {
		return body.Name
	}
	return ""
}

// auditDeniedOp maps a mutating request to its audit operation. Reads are
// never audited, so GET requests and unmapped paths report ok=false.
func auditDeniedOp(method, path string, identity *auth.UserIdentity) (op, appName, targetType, targetID string, ok bool) {
	switch method {
	case "POST", "PUT", "PATCH", "DELETE":
	default:
		return "", "", "", "", false
	}
	segs := strings.Split(strings.Trim(path, "/"), "/")
	if len(segs) < 3 || segs[0] != "v2" {
		return "", "", "", "", false
	}
	if segs[1] == "users" {
		return "", "", "", "", false
	}
	if segs[1] != "orgs" {
		return "", "", "", "", false
	}
	rest := segs[2:]

	// Organization-level routes: /v2/orgs/{org}[/...].
	if len(rest) == 1 {
		if method == "PATCH" {
			return auditOpOrgUpdate, "", string(gen.AuditTargetTypeOrganization), rest[0], true
		}
		return "", "", "", "", false
	}
	switch rest[1] {
	case "join":
		if method == "POST" {
			username := ""
			if identity != nil {
				username = identity.Username
			}
			return auditOpUserJoin, "", string(gen.AuditTargetTypeUser), username, true
		}
	case "secrets":
		// Join-secret generation is enforced at the handler; the
		// middleware records the denial for audit continuity.
		if len(rest) == 2 && method == "POST" {
			return auditOpSecretGenerate, "", string(gen.AuditTargetTypeOrganization), rest[0], true
		}
	case "members":
		if len(rest) == 3 {
			if method == "DELETE" {
				return auditOpUserRemove, "", string(gen.AuditTargetTypeUser), rest[2], true
			}
		} else if len(rest) == 5 && rest[3] == "roles" && method == "PUT" {
			return auditOpRoleGrant, "", string(gen.AuditTargetTypeUser), rest[2], true
		}
	case "roles":
		if len(rest) == 2 && method == "POST" {
			return auditOpRoleCreate, "", string(gen.AuditTargetTypeRole), "", true
		}
		if len(rest) == 3 && method == "DELETE" {
			return auditOpRoleDelete, "", string(gen.AuditTargetTypeRole), rest[2], true
		}
	case "tokens":
		// Token routes always carry the key name: POST creates and
		// DELETE revokes /v2/orgs/{org}/tokens/{tokenName}. A future
		// nameless POST would fall through to no mapping rather than
		// mis-attribute, by construction of the len(rest) == 3 check.
		if len(rest) == 3 && (method == "POST" || method == "DELETE") {
			op := auditOpTokenCreate
			if method == "DELETE" {
				op = auditOpTokenRevoke
			}
			return op, "", string(gen.AuditTargetTypeToken), rest[2], true
		}
	case "domain-claims":
		if len(rest) == 2 && method == "POST" {
			return auditOpDomainClaimCreate, "", string(gen.AuditTargetTypeDomainClaim), "", true
		}
		if len(rest) == 3 && method == "DELETE" {
			return auditOpDomainClaimDelete, "", string(gen.AuditTargetTypeDomainClaim), rest[2], true
		}
	case "apps":
		if len(rest) < 3 {
			return "", "", "", "", false
		}
		app := rest[2]
		rest := rest[3:]
		if len(rest) == 0 {
			switch method {
			case "PUT":
				return auditOpAppCreate, app, string(gen.AuditTargetTypeApplication), app, true
			case "PATCH":
				return auditOpAppUpdate, app, string(gen.AuditTargetTypeApplication), app, true
			case "DELETE":
				return auditOpAppDelete, app, string(gen.AuditTargetTypeApplication), app, true
			}
			return "", "", "", "", false
		}
		switch rest[0] {
		case "versions":
			if len(rest) == 2 && rest[1] == "latest" && method == "PATCH" {
				return auditOpAppSetLatest, app, string(gen.AuditTargetTypeApplication), app, true
			}
		case "workflows":
			return auditDeniedWorkflowOp(method, rest[1:], app)
		case "schedules":
			if len(rest) == 3 && method == "POST" {
				var op string
				switch rest[2] {
				case "pause":
					op = auditOpSchedulePause
				case "resume":
					op = auditOpScheduleResume
				case "trigger":
					op = auditOpScheduleTrigger
				case "backfill":
					op = auditOpScheduleBackfill
				}
				if op != "" {
					return op, app, string(gen.AuditTargetTypeSchedule), rest[1], true
				}
			}
		case "alerting-rules":
			if len(rest) == 1 && method == "POST" {
				return auditOpAlertRuleCreate, app, string(gen.AuditTargetTypeAlertingRule), "", true
			}
			if len(rest) == 2 && method == "DELETE" {
				return auditOpAlertRuleDelete, app, string(gen.AuditTargetTypeAlertingRule), rest[1], true
			}
		}
	}
	return "", "", "", "", false
}

func auditDeniedWorkflowOp(method string, rest []string, app string) (op, appName, targetType, targetID string, ok bool) {
	if len(rest) == 0 {
		return "", "", "", "", false
	}
	if rest[0] == "bulk-cancel" && method == "POST" {
		return auditOpWorkflowBulkCancel, app, "", "", true
	}
	if rest[0] == "bulk-resume" && method == "POST" {
		return auditOpWorkflowBulkResume, app, "", "", true
	}
	if rest[0] == "bulk-delete" && method == "POST" {
		return auditOpWorkflowBulkDelete, app, "", "", true
	}
	if rest[0] == "bulk-fork-from-failure" && method == "POST" {
		return auditOpWorkflowForkFail, app, "", "", true
	}
	if rest[0] == "import" && method == "POST" {
		return auditOpWorkflowImport, app, "", "", true
	}
	if len(rest) == 1 {
		if method == "DELETE" {
			return auditOpWorkflowDelete, app, string(gen.AuditTargetTypeWorkflow), rest[0], true
		}
		return "", "", "", "", false
	}
	if len(rest) == 2 && method == "POST" {
		switch rest[1] {
		case "cancel":
			return auditOpWorkflowCancel, app, string(gen.AuditTargetTypeWorkflow), rest[0], true
		case "resume":
			return auditOpWorkflowResume, app, string(gen.AuditTargetTypeWorkflow), rest[0], true
		case "fork":
			return auditOpWorkflowFork, app, string(gen.AuditTargetTypeWorkflow), rest[0], true
		}
	}
	return "", "", "", "", false
}

// recordMiddlewareDenial records a failure entry when AuthMiddleware denies
// a mutating request after identity is established. Requests without
// identity (authentication failures) and unmapped paths are skipped.
func recordMiddlewareDenial(r *http.Request, server *Server, identity *auth.UserIdentity) {
	if server == nil || identity == nil {
		return
	}
	op, appName, targetType, targetID, ok := auditDeniedOp(r.Method, r.URL.Path, identity)
	if !ok {
		return
	}
	orgName := r.PathValue("orgName")
	server.auditOperationAs(r.Context(), identity, orgName, appName, op, auditStatusFailure, targetType, targetID, nil)
}

type AuditRetentionStore interface {
	ListAllOrganisations(ctx context.Context) ([]storegen.Organisation, error)
	DeleteExpiredAuditLogs(ctx context.Context, arg storegen.DeleteExpiredAuditLogsParams) (int64, error)
}

// AuditRetentionSweeper purges audit entries older than each
// organisation's configured retention window.
type AuditRetentionSweeper struct {
	store  AuditRetentionStore
	logger *slog.Logger
	wg     sync.WaitGroup
}

// NewAuditRetentionSweeper creates a sweeper over store.
func NewAuditRetentionSweeper(store AuditRetentionStore, logger *slog.Logger) *AuditRetentionSweeper {
	if logger == nil {
		logger = slog.Default()
	}
	return &AuditRetentionSweeper{store: store, logger: logger}
}

// SweepOnce deletes entries older than the retention window for every
// organisation and returns the total rows removed.
func (s *AuditRetentionSweeper) SweepOnce(ctx context.Context) (int64, error) {
	orgs, err := s.store.ListAllOrganisations(ctx)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, org := range orgs {
		days := int64(org.AuditLogRetentionDays)
		if days < MinAuditLogRetentionDays || days > MaxAuditLogRetentionDays {
			days = DefaultAuditLogRetentionDays
		}
		cutoff := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
		n, err := s.store.DeleteExpiredAuditLogs(ctx, storegen.DeleteExpiredAuditLogsParams{
			OrganisationID: org.ID,
			Cutoff:         pgtype.Timestamptz{Time: cutoff, Valid: true},
		})
		if err != nil {
			s.logger.Warn("audit retention sweep failed", "org", org.Name, "error", err)
			continue
		}
		total += n
	}
	return total, nil
}

// Start launches a background sweep loop; the returned func stops it.
func (s *AuditRetentionSweeper) Start(ctx context.Context, interval time.Duration) func() {
	if interval <= 0 {
		interval = auditSweepInterval
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.wg.Add(1)
	safego.Go(s.logger, "audit-retention-sweep", func() {
		defer s.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				if _, err := s.SweepOnce(runCtx); err != nil {
					s.logger.Warn("audit retention sweep encountered error", "error", err)
				}
			}
		}
	})
	return func() {
		cancel()
		s.wg.Wait()
	}
}
