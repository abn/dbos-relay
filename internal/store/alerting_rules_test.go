package store

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/store/gen"
)

func TestAlertingRules(t *testing.T) {
	s := testStore(t)
	if s == nil {
		t.Skip("skipping test; no database")
	}

	ctx := context.Background()

	org, err := s.Queries().CreateOrganisation(ctx, "alert_org")
	if err != nil {
		t.Fatalf("CreateOrganisation failed: %v", err)
	}

	app1, err := s.Queries().CreateApplication(ctx, gen.CreateApplicationParams{
		OrganisationID: org.ID,
		Name:           "alert-app-1",
		Settings:       []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("CreateApplication 1 failed: %v", err)
	}

	app2, err := s.Queries().CreateApplication(ctx, gen.CreateApplicationParams{
		OrganisationID: org.ID,
		Name:           "alert-app-2",
		Settings:       []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("CreateApplication 2 failed: %v", err)
	}

	var ruleID pgtype.UUID

	t.Run("CreateAlertingRule", func(t *testing.T) {
		interval := int32(60)
		rule, err := s.Queries().CreateAlertingRule(ctx, gen.CreateAlertingRuleParams{
			ApplicationID:          app1.ID,
			ReceivingApplicationID: app2.ID,
			RuleType:               "WorkflowFailure",
			RuleMetadata:           []byte(`{"threshold":"3"}`),
			MinIntervalSecs:        &interval,
		})
		if err != nil {
			t.Fatalf("CreateAlertingRule failed: %v", err)
		}
		ruleID = rule.ID
		if rule.RuleType != "WorkflowFailure" {
			t.Errorf("expected ruleType 'WorkflowFailure', got %q", rule.RuleType)
		}
		if rule.MinIntervalSecs == nil || *rule.MinIntervalSecs != 60 {
			t.Errorf("expected minIntervalSecs 60, got %v", rule.MinIntervalSecs)
		}
	})

	t.Run("GetAlertingRule", func(t *testing.T) {
		rule, err := s.Queries().GetAlertingRule(ctx, gen.GetAlertingRuleParams{
			ID:            ruleID,
			ApplicationID: app1.ID,
		})
		if err != nil {
			t.Fatalf("GetAlertingRule failed: %v", err)
		}
		if rule.ID != ruleID {
			t.Errorf("expected rule ID %v, got %v", ruleID, rule.ID)
		}
	})

	t.Run("ListAlertingRulesByApplication", func(t *testing.T) {
		rules, err := s.Queries().ListAlertingRulesByApplication(ctx, app1.ID)
		if err != nil {
			t.Fatalf("ListAlertingRulesByApplication failed: %v", err)
		}
		if len(rules) != 1 {
			t.Errorf("expected 1 rule, got %d", len(rules))
		}
	})

	t.Run("TouchAlertRuleLastFired", func(t *testing.T) {
		err := s.Queries().TouchAlertRuleLastFired(ctx, ruleID)
		if err != nil {
			t.Fatalf("TouchAlertRuleLastFired failed: %v", err)
		}
		rule, err := s.Queries().GetAlertingRule(ctx, gen.GetAlertingRuleParams{
			ID:            ruleID,
			ApplicationID: app1.ID,
		})
		if err != nil {
			t.Fatalf("GetAlertingRule after touch failed: %v", err)
		}
		if !rule.LastFiredAt.Valid {
			t.Errorf("expected LastFiredAt to be valid after touch")
		}
	})

	t.Run("DeleteAlertingRule", func(t *testing.T) {
		rows, err := s.Queries().DeleteAlertingRule(ctx, gen.DeleteAlertingRuleParams{
			ID:            ruleID,
			ApplicationID: app1.ID,
		})
		if err != nil {
			t.Fatalf("DeleteAlertingRule failed: %v", err)
		}
		if rows != 1 {
			t.Errorf("expected 1 row deleted, got %d", rows)
		}
		_, err = s.Queries().GetAlertingRule(ctx, gen.GetAlertingRuleParams{
			ID:            ruleID,
			ApplicationID: app1.ID,
		})
		if err == nil {
			t.Errorf("expected error getting deleted rule, got nil")
		}
	})
}
