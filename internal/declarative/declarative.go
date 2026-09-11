// Package declarative manages declarative Relay configuration manifests (relay.yaml).
package declarative

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"gopkg.in/yaml.v3"

	"github.com/abn/relay/internal/auth"
	"github.com/abn/relay/internal/store"
	"github.com/abn/relay/internal/store/gen"
)

var appNameRegex = regexp.MustCompile(`^[a-z0-9\-_]{3,256}$`)

// Config represents a declarative Relay configuration document.
type Config struct {
	Version      string                `yaml:"version"`
	Organisation string                `yaml:"organisation,omitempty"`
	Applications []Application         `yaml:"applications,omitempty"`
	AlertRules   []AlertRule           `yaml:"alert_rules,omitempty"`
	DataPlanes   map[string]DataPlane  `yaml:"data_plane,omitempty"`
}

// Application represents an application definition.
type Application struct {
	Name                string `yaml:"name"`
	Description         string `yaml:"description,omitempty"`
	StuckSLASecs        int32  `yaml:"stuck_sla_secs,omitempty"`
	ExecutorTimeoutSecs int64  `yaml:"executor_timeout_secs,omitempty"`
}

// AlertRule represents a declarative alerting rule.
type AlertRule struct {
	App             string         `yaml:"app"`
	RuleType        string         `yaml:"rule_type"`
	ReceivingApp    string         `yaml:"receiving_app,omitempty"`
	MinIntervalSecs int32          `yaml:"min_interval_secs,omitempty"`
	Metadata        map[string]any `yaml:"metadata,omitempty"`
}

// ConnectionSource specifies dynamic resolution of a secret connection string.
type ConnectionSource struct {
	Env  string `yaml:"env,omitempty"`
	File string `yaml:"file,omitempty"`
}

// DataPlane defines data-plane connection settings for an application.
type DataPlane struct {
	ConnectionURL        string            `yaml:"connection_url,omitempty"`
	ConnectionStringFrom *ConnectionSource `yaml:"connection_string_from,omitempty"`
	Mode                 string            `yaml:"mode,omitempty"`
	StatementTimeoutSecs int               `yaml:"statement_timeout_secs,omitempty"`
	MaxConnections       int               `yaml:"max_connections,omitempty"`
}

// ResolveConnectionURL resolves the connection URL expanding environment variables or reading secret files.
func (dp *DataPlane) ResolveConnectionURL() (string, error) {
	if dp.ConnectionStringFrom != nil {
		if dp.ConnectionStringFrom.Env != "" {
			val := os.Getenv(dp.ConnectionStringFrom.Env)
			if val == "" {
				return "", fmt.Errorf("environment variable %q is not set", dp.ConnectionStringFrom.Env)
			}
			return val, nil
		}
		if dp.ConnectionStringFrom.File != "" {
			b, err := os.ReadFile(dp.ConnectionStringFrom.File)
			if err != nil {
				return "", fmt.Errorf("reading secret file %q: %w", dp.ConnectionStringFrom.File, err)
			}
			return strings.TrimSpace(string(b)), nil
		}
	}
	if dp.ConnectionURL != "" {
		return os.ExpandEnv(dp.ConnectionURL), nil
	}
	return "", errors.New("neither connection_url nor connection_string_from provided")
}

func resolveDestinations(meta map[string]any) (map[string]any, error) {
	if meta == nil {
		return nil, nil
	}
	destsRaw, ok := meta["destinations"]
	if !ok {
		return meta, nil
	}
	destsSlice, ok := destsRaw.([]any)
	if !ok {
		return meta, nil
	}
	newMeta := make(map[string]any, len(meta))
	for k, v := range meta {
		newMeta[k] = v
	}
	var resolvedDests []any
	for _, dRaw := range destsSlice {
		dMap, ok := dRaw.(map[string]any)
		if !ok {
			resolvedDests = append(resolvedDests, dRaw)
			continue
		}
		newDMap := make(map[string]any, len(dMap))
		for k, v := range dMap {
			newDMap[k] = v
		}
		if sfRaw, ok := dMap["secret_from"]; ok {
			var cs ConnectionSource
			sfBytes, _ := json.Marshal(sfRaw)
			if err := json.Unmarshal(sfBytes, &cs); err == nil {
				if cs.Env != "" {
					val := os.Getenv(cs.Env)
					if val == "" {
						return nil, fmt.Errorf("environment variable %q is not set", cs.Env)
					}
					newDMap["secret"] = val
				} else if cs.File != "" {
					b, err := os.ReadFile(cs.File)
					if err != nil {
						return nil, fmt.Errorf("reading secret file %q: %w", cs.File, err)
					}
					newDMap["secret"] = strings.TrimSpace(string(b))
				}
				delete(newDMap, "secret_from")
			}
		}
		resolvedDests = append(resolvedDests, newDMap)
	}
	newMeta["destinations"] = resolvedDests
	return newMeta, nil
}

// DiffAction indicates the change action for a resource.
type DiffAction string

const (
	ActionCreate    DiffAction = "CREATE"
	ActionUnchanged DiffAction = "UNCHANGED"
	ActionDelete    DiffAction = "DELETE"
)

// DiffItem describes a single difference between desired and actual state.
type DiffItem struct {
	Action  DiffAction
	Kind    string
	Name    string
	Details string
}

// Plan holds the set of reconciliation actions.
type Plan struct {
	Items         []DiffItem
	GeneratedKeys map[string]string
}

// Summary returns a short summary of plan actions.
func (p *Plan) Summary() string {
	var creates, deletes, unchanged int
	for _, item := range p.Items {
		switch item.Action {
		case ActionCreate:
			creates++
		case ActionDelete:
			deletes++
		case ActionUnchanged:
			unchanged++
		}
	}
	return fmt.Sprintf("Plan: %d to create, %d to delete, %d unchanged.", creates, deletes, unchanged)
}

func (p *Plan) String() string {
	var b strings.Builder
	for _, item := range p.Items {
		var sym string
		switch item.Action {
		case ActionCreate:
			sym = "+"
		case ActionDelete:
			sym = "-"
		case ActionUnchanged:
			sym = " "
		}
		if item.Details != "" {
			fmt.Fprintf(&b, "%s [%s] %s (%s): %s\n", sym, item.Action, item.Kind, item.Name, item.Details)
		} else {
			fmt.Fprintf(&b, "%s [%s] %s (%s)\n", sym, item.Action, item.Kind, item.Name)
		}
	}
	return b.String()
}

// Parse decodes YAML data into a Config.
func Parse(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing configuration yaml: %w", err)
	}
	if cfg.Organisation == "" {
		cfg.Organisation = "default"
	}
	return &cfg, nil
}

// LoadFile reads and parses a YAML file from the filesystem.
func LoadFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading file %s: %w", path, err)
	}
	cfg, err := Parse(data)
	if err != nil {
		return nil, err
	}
	if err := Validate(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Load reads and parses YAML data from an io.Reader.
func Load(r io.Reader) (*Config, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading configuration: %w", err)
	}
	cfg, err := Parse(data)
	if err != nil {
		return nil, err
	}
	if err := Validate(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate checks configuration invariants.
func Validate(cfg *Config) error {
	if cfg.Version != "1" && cfg.Version != "v1" {
		return fmt.Errorf("unsupported configuration version %q, expected '1'", cfg.Version)
	}

	appSet := make(map[string]bool)
	for _, app := range cfg.Applications {
		if !appNameRegex.MatchString(app.Name) {
			return fmt.Errorf("invalid application name %q: must match %s", app.Name, appNameRegex.String())
		}
		if appSet[app.Name] {
			return fmt.Errorf("duplicate application name %q", app.Name)
		}
		appSet[app.Name] = true
	}

	for _, rule := range cfg.AlertRules {
		if rule.App == "" {
			return errors.New("alert rule missing required field 'app'")
		}
		switch rule.RuleType {
		case "UnresponsiveApplication", "WorkflowFailure", "SlowQueue", "RecoveryFlapping", "StrandedVersion":
			// valid
		default:
			return fmt.Errorf("unknown alert rule type %q", rule.RuleType)
		}
	}

	for appName, dp := range cfg.DataPlanes {
		if dp.ConnectionURL == "" && dp.ConnectionStringFrom == nil {
			return fmt.Errorf("data_plane for app %q requires connection_url or connection_string_from", appName)
		}
		if dp.Mode != "" && dp.Mode != "read" && dp.Mode != "read-write" {
			return fmt.Errorf("invalid data_plane mode %q for app %q (must be 'read' or 'read-write')", dp.Mode, appName)
		}
	}

	return nil
}

// Diff compares the desired configuration with current database state.
func Diff(ctx context.Context, s *store.Store, cfg *Config) (*Plan, error) {
	var items []DiffItem

	orgName := cfg.Organisation
	org, err := s.Queries().GetOrganisationByName(ctx, orgName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			items = append(items, DiffItem{
				Action: ActionCreate,
				Kind:   "Organisation",
				Name:   orgName,
			})
		} else {
			return nil, fmt.Errorf("fetching organisation %s: %w", orgName, err)
		}
	} else {
		items = append(items, DiffItem{
			Action: ActionUnchanged,
			Kind:   "Organisation",
			Name:   orgName,
		})
	}

	existingApps := make(map[string]gen.Application)
	if org.ID.Valid {
		apps, err := s.Queries().ListApplicationsByOrganisation(ctx, org.ID)
		if err != nil {
			return nil, fmt.Errorf("listing applications: %w", err)
		}
		for _, a := range apps {
			existingApps[a.Name] = a
		}
	}

	for _, app := range cfg.Applications {
		if _, exists := existingApps[app.Name]; exists {
			items = append(items, DiffItem{
				Action: ActionUnchanged,
				Kind:   "Application",
				Name:   app.Name,
			})
		} else {
			items = append(items, DiffItem{
				Action: ActionCreate,
				Kind:   "Application",
				Name:   app.Name,
			})
		}
	}

	for _, rule := range cfg.AlertRules {
		app, exists := existingApps[rule.App]
		if !exists {
			items = append(items, DiffItem{
				Action:  ActionCreate,
				Kind:    "AlertRule",
				Name:    fmt.Sprintf("%s/%s", rule.App, rule.RuleType),
				Details: "application not yet created",
			})
			continue
		}

		rules, err := s.Queries().ListAlertingRulesByApplication(ctx, app.ID)
		if err != nil {
			return nil, fmt.Errorf("listing rules for %s: %w", rule.App, err)
		}

		found := false
		for _, r := range rules {
			if r.RuleType == rule.RuleType {
				found = true
				break
			}
		}

		if found {
			items = append(items, DiffItem{
				Action: ActionUnchanged,
				Kind:   "AlertRule",
				Name:   fmt.Sprintf("%s/%s", rule.App, rule.RuleType),
			})
		} else {
			items = append(items, DiffItem{
				Action: ActionCreate,
				Kind:   "AlertRule",
				Name:   fmt.Sprintf("%s/%s", rule.App, rule.RuleType),
			})
		}
	}

	for appName, dp := range cfg.DataPlanes {
		mode := dp.Mode
		if mode == "" {
			mode = "read"
		}
		items = append(items, DiffItem{
			Action:  ActionUnchanged,
			Kind:    "DataPlane",
			Name:    appName,
			Details: fmt.Sprintf("mode=%s, dsn=[REDACTED]", mode),
		})
	}

	return &Plan{Items: items}, nil
}

// Apply reconciles the database state with the desired configuration.
func Apply(ctx context.Context, s *store.Store, cfg *Config) (*Plan, error) {
	var items []DiffItem
	generatedKeys := make(map[string]string)

	orgName := cfg.Organisation
	org, err := s.Queries().GetOrganisationByName(ctx, orgName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			org, err = s.Queries().CreateOrganisation(ctx, orgName)
			if err != nil {
				return nil, fmt.Errorf("creating organisation %s: %w", orgName, err)
			}
			items = append(items, DiffItem{Action: ActionCreate, Kind: "Organisation", Name: orgName})
		} else {
			return nil, fmt.Errorf("getting organisation %s: %w", orgName, err)
		}
	} else {
		items = append(items, DiffItem{Action: ActionUnchanged, Kind: "Organisation", Name: orgName})
	}

	// Ensure default API key exists for conductor WebSocket connections
	if org.ID.Valid {
		keys, err := s.Queries().ListAPIKeys(ctx, org.ID)
		if err == nil && len(keys) == 0 {
			var rawKey string
			var record auth.KeyRecord
			if envKey := os.Getenv("RELAY_API_KEY"); envKey != "" {
				rawKey = envKey
				record = auth.KeyRecord{
					Lookup: auth.Lookup(rawKey),
					Hash:   auth.Hash(rawKey),
				}
			} else {
				var mintErr error
				rawKey, record, mintErr = auth.Mint()
				if mintErr != nil {
					return nil, fmt.Errorf("minting default api key: %w", mintErr)
				}
			}
			_, err = s.Queries().CreateAPIKey(ctx, gen.CreateAPIKeyParams{
				OrganisationID:   org.ID,
				Name:             "default-conductor-key",
				Lookup:           record.Lookup,
				KeyHash:          record.Hash,
				ApplicationNames: []string{},
				Permissions:      []string{"application.read", "application.write", "websocket.connect"},
			})
			if err != nil {
				return nil, fmt.Errorf("creating default api key: %w", err)
			}
			generatedKeys["default-conductor-key"] = rawKey
			items = append(items, DiffItem{
				Action:  ActionCreate,
				Kind:    "APIKey",
				Name:    "default-conductor-key",
				Details: fmt.Sprintf("minted key %s", auth.Lookup(rawKey)+"***"),
			})
		}
	}

	apps, err := s.Queries().ListApplicationsByOrganisation(ctx, org.ID)
	if err != nil {
		return nil, fmt.Errorf("listing applications: %w", err)
	}
	appMap := make(map[string]gen.Application)
	for _, a := range apps {
		appMap[a.Name] = a
	}

	for _, app := range cfg.Applications {
		settingsMap := make(map[string]any)
		if app.Description != "" {
			settingsMap["description"] = app.Description
		}
		if app.StuckSLASecs > 0 {
			settingsMap["stuck_sla_secs"] = app.StuckSLASecs
		}
		if app.ExecutorTimeoutSecs > 0 {
			settingsMap["executorTimeoutSecs"] = app.ExecutorTimeoutSecs
		}
		settingsJSON, _ := json.Marshal(settingsMap)

		if _, exists := appMap[app.Name]; exists {
			updated, err := s.Queries().UpdateApplicationSettings(ctx, gen.UpdateApplicationSettingsParams{
				OrganisationID: org.ID,
				Name:           app.Name,
				Settings:       settingsJSON,
			})
			if err != nil {
				return nil, fmt.Errorf("updating application settings %s: %w", app.Name, err)
			}
			appMap[app.Name] = updated
			items = append(items, DiffItem{Action: ActionUnchanged, Kind: "Application", Name: app.Name})
		} else {
			created, err := s.Queries().CreateApplication(ctx, gen.CreateApplicationParams{
				OrganisationID: org.ID,
				Name:           app.Name,
				Settings:       settingsJSON,
			})
			if err != nil {
				return nil, fmt.Errorf("creating application %s: %w", app.Name, err)
			}
			appMap[app.Name] = created
			items = append(items, DiffItem{Action: ActionCreate, Kind: "Application", Name: app.Name})
		}
	}

	for _, rule := range cfg.AlertRules {
		app, exists := appMap[rule.App]
		if !exists {
			return nil, fmt.Errorf("rule refers to non-existent application %q", rule.App)
		}

		recvAppID := app.ID
		if rule.ReceivingApp != "" && rule.ReceivingApp != rule.App {
			recvApp, exists := appMap[rule.ReceivingApp]
			if !exists {
				return nil, fmt.Errorf("rule receiving_app %q not found", rule.ReceivingApp)
			}
			recvAppID = recvApp.ID
		}

		existingRules, err := s.Queries().ListAlertingRulesByApplication(ctx, app.ID)
		if err != nil {
			return nil, fmt.Errorf("listing rules for %s: %w", rule.App, err)
		}

		var matchingRule *gen.AlertingRule
		for _, r := range existingRules {
			if r.RuleType == rule.RuleType && r.ReceivingApplicationID == recvAppID {
				matchingRule = &r
				break
			}
		}

		if matchingRule != nil {
			items = append(items, DiffItem{
				Action: ActionUnchanged,
				Kind:   "AlertRule",
				Name:   fmt.Sprintf("%s/%s", rule.App, rule.RuleType),
			})
		} else {
			metaBytes := []byte("{}")
			metaMap, err := resolveDestinations(rule.Metadata)
			if err != nil {
				return nil, fmt.Errorf("resolving rule destinations for %s: %w", rule.RuleType, err)
			}
			if len(metaMap) > 0 {
				metaBytes, _ = json.Marshal(metaMap)
			}
			var minInterval *int32
			if rule.MinIntervalSecs > 0 {
				minInterval = &rule.MinIntervalSecs
			}
			var createErr error
			_, createErr = s.Queries().CreateAlertingRule(ctx, gen.CreateAlertingRuleParams{
				ApplicationID:          app.ID,
				ReceivingApplicationID: recvAppID,
				RuleType:               rule.RuleType,
				RuleMetadata:           metaBytes,
				MinIntervalSecs:        minInterval,
			})
			if createErr != nil {
				return nil, fmt.Errorf("creating rule %s: %w", rule.RuleType, createErr)
			}
			items = append(items, DiffItem{
				Action: ActionCreate,
				Kind:   "AlertRule",
				Name:   fmt.Sprintf("%s/%s", rule.App, rule.RuleType),
			})
		}
	}

	for appName, dp := range cfg.DataPlanes {
		mode := dp.Mode
		if mode == "" {
			mode = "read"
		}
		items = append(items, DiffItem{
			Action:  ActionUnchanged,
			Kind:    "DataPlane",
			Name:    appName,
			Details: fmt.Sprintf("mode=%s", mode),
		})
	}

	return &Plan{Items: items, GeneratedKeys: generatedKeys}, nil
}
