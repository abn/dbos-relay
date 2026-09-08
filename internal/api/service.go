// Package api serves Relay's HTTP endpoints, including health checks,
// the vendored OpenAPI specifications, interactive documentation, and schemas.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/abn/relay/api/spec"
	"github.com/abn/relay/internal/problem"
)

// Pinger verifies database connectivity.
type Pinger interface {
	Ping(ctx context.Context) error
}

var (
	openapiJSON      []byte
	openapi30JSON    []byte
	openapiYAML      []byte
	schemaComponents map[string]json.RawMessage
)

func init() {
	var err error
	openapiJSON, err = spec.FS.ReadFile("openapi.json")
	if err != nil {
		panic(fmt.Sprintf("reading embedded openapi.json: %v", err))
	}

	openapi30JSON, err = spec.FS.ReadFile("openapi-3.0.json")
	if err != nil {
		panic(fmt.Sprintf("reading embedded openapi-3.0.json: %v", err))
	}

	var parsed any
	if err := yaml.Unmarshal(openapiJSON, &parsed); err != nil {
		panic(fmt.Sprintf("parsing openapi.json to yaml: %v", err))
	}
	openapiYAML, err = yaml.Marshal(parsed)
	if err != nil {
		panic(fmt.Sprintf("marshaling openapi.yaml: %v", err))
	}

	var doc struct {
		Components struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(openapiJSON, &doc); err != nil {
		panic(fmt.Sprintf("parsing components.schemas from openapi.json: %v", err))
	}
	schemaComponents = doc.Components.Schemas
}

const docsHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Relay API Documentation</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css" />
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js" crossorigin="anonymous"></script>
  <script>
    window.onload = () => {
      window.ui = SwaggerUIBundle({
        url: '/openapi.json',
        dom_id: '#swagger-ui',
      });
    };
  </script>
</body>
</html>
`

// NewHandler constructs an http.Handler with all unauthenticated base routes.
func NewHandler(db Pinger) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if db == nil {
			problem.Write(w, &problem.Problem{
				Title:  "database unavailable",
				Status: http.StatusServiceUnavailable,
				Detail: "database handle is nil",
			})
			return
		}
		if err := db.Ping(r.Context()); err != nil {
			problem.Write(w, &problem.Problem{
				Title:  "database unavailable",
				Status: http.StatusServiceUnavailable,
				Detail: err.Error(),
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{\"status\":\"ok\"}\n"))
	})

	mux.HandleFunc("/openapi.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(openapiJSON)
	})

	mux.HandleFunc("/openapi-3.0.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(openapi30JSON)
	})

	mux.HandleFunc("/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(openapiYAML)
	})

	mux.HandleFunc("/docs", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(docsHTML))
	})

	mux.HandleFunc("/schemas/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/schemas/")
		name = strings.TrimSuffix(name, ".json")
		if name != "" {
			if schema, ok := schemaComponents[name]; ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(schema)
				return
			}
		}
		http.Redirect(w, r, "/openapi.json#/components/schemas", http.StatusSeeOther)
	})

	return mux
}
