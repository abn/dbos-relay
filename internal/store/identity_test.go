package store

import (
	"context"
	"testing"

	"github.com/abn/relay/internal/store/gen"
)

func TestIdentityStore(t *testing.T) {
	s := testStore(t)
	if s == nil {
		t.Skip("skipping test; no database")
	}

	ctx := context.Background()

	// 1. Verify global roles are seeded
	org, err := s.Queries().CreateOrganisation(ctx, "acme_identity")
	if err != nil {
		t.Fatalf("CreateOrganisation: %v", err)
	}

	roles, err := s.Queries().ListRoles(ctx, org.ID)
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	roleMap := make(map[string]gen.Role)
	for _, r := range roles {
		roleMap[r.Name] = r
	}
	for _, expected := range []string{"admin", "operator", "viewer"} {
		r, ok := roleMap[expected]
		if !ok {
			t.Errorf("expected global role %q to be present", expected)
		} else if !r.IsGlobal {
			t.Errorf("role %q isGlobal = false, want true", expected)
		}
	}

	// 2. User creation and upsert
	u1, err := s.Queries().CreateUser(ctx, gen.CreateUserParams{
		Subject:  "auth0|user123",
		Username: "alice",
		Email:    "alice@example.com",
		IsAdmin:  false,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u1.Username != "alice" || u1.Email != "alice@example.com" {
		t.Errorf("unexpected user: %+v", u1)
	}

	// Upsert updates email and username
	u1Updated, err := s.Queries().UpsertUser(ctx, gen.UpsertUserParams{
		Subject:  "auth0|user123",
		Username: "alice_new",
		Email:    "alice_new@example.com",
		IsAdmin:  false,
	})
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	if u1Updated.Username != "alice_new" || u1Updated.Email != "alice_new@example.com" {
		t.Errorf("unexpected updated user: %+v", u1Updated)
	}

	// 3. Organisation membership
	mem, err := s.Queries().UpsertMemberRole(ctx, gen.UpsertMemberRoleParams{
		OrganisationID: org.ID,
		UserID:         u1.ID,
		RoleName:       "admin",
	})
	if err != nil {
		t.Fatalf("UpsertMemberRole: %v", err)
	}
	if mem.RoleName != "admin" {
		t.Errorf("roleName = %q, want admin", mem.RoleName)
	}

	primaryOrg, err := s.Queries().GetUserPrimaryOrganisation(ctx, u1.ID)
	if err != nil {
		t.Fatalf("GetUserPrimaryOrganisation: %v", err)
	}
	if primaryOrg.Name != "acme_identity" || primaryOrg.RoleName != "admin" {
		t.Errorf("unexpected primary org: %+v", primaryOrg)
	}

	members, err := s.Queries().ListMembersByOrganisation(ctx, org.ID)
	if err != nil {
		t.Fatalf("ListMembersByOrganisation: %v", err)
	}
	if len(members) != 1 || members[0].Username != "alice_new" {
		t.Errorf("unexpected members: %+v", members)
	}

	// 4. Domain claims
	dc, err := s.Queries().CreateDomainClaim(ctx, gen.CreateDomainClaimParams{
		OrganisationID: org.ID,
		Domain:         "example.com",
	})
	if err != nil {
		t.Fatalf("CreateDomainClaim: %v", err)
	}
	if dc.Domain != "example.com" {
		t.Errorf("domain = %q, want example.com", dc.Domain)
	}

	dcFound, err := s.Queries().GetDomainClaim(ctx, "example.com")
	if err != nil {
		t.Fatalf("GetDomainClaim: %v", err)
	}
	if dcFound.OrganisationID != org.ID {
		t.Errorf("dcFound org ID mismatch: %+v", dcFound)
	}

	claims, err := s.Queries().ListDomainClaims(ctx, org.ID)
	if err != nil {
		t.Fatalf("ListDomainClaims: %v", err)
	}
	if len(claims) != 1 {
		t.Errorf("expected 1 claim, got %d", len(claims))
	}

	if _, err := s.Queries().DeleteDomainClaim(ctx, gen.DeleteDomainClaimParams{
		OrganisationID: org.ID,
		Domain:         "example.com",
	}); err != nil {
		t.Fatalf("DeleteDomainClaim: %v", err)
	}

	// 5. Audit logs
	al, err := s.Queries().CreateAuditLog(ctx, gen.CreateAuditLogParams{
		OrganisationID: org.ID,
		UserID:         u1.ID,
		Username:       "alice_new",
		Action:         "role.create",
		Details:        []byte(`{"role":"custom_role"}`),
	})
	if err != nil {
		t.Fatalf("CreateAuditLog: %v", err)
	}
	if al.Action != "role.create" {
		t.Errorf("action = %q, want role.create", al.Action)
	}

	logs, err := s.Queries().ListAuditLogs(ctx, gen.ListAuditLogsParams{
		OrganisationID: org.ID,
		Limit:          10,
		Offset:         0,
	})
	if err != nil {
		t.Fatalf("ListAuditLogs: %v", err)
	}
	if len(logs) != 1 || logs[0].Action != "role.create" {
		t.Errorf("unexpected logs: %+v", logs)
	}
}
