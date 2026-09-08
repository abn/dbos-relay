// Package api serves Relay's HTTP endpoints, including health checks,
// the vendored OpenAPI specifications, interactive documentation, and schemas.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/abn/relay/api/spec"
	"github.com/abn/relay/internal/api/gen"
	"github.com/abn/relay/internal/problem"
	storegen "github.com/abn/relay/internal/store/gen"
	"github.com/jackc/pgx/v5/pgtype"
)

// ExecutorReader provides database queries for the executor endpoint.
type ExecutorReader interface {
	GetOrganisationByName(ctx context.Context, name string) (storegen.Organisation, error)
	GetApplicationByName(ctx context.Context, arg storegen.GetApplicationByNameParams) (storegen.Application, error)
	ListExecutorsByApplication(ctx context.Context, applicationID pgtype.UUID) ([]storegen.Executor, error)
}

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
	openapiJSON, err = spec.OpenAPISpec()
	if err != nil {
		panic(fmt.Sprintf("reading openapi spec: %v", err))
	}

	openapi30JSON, err = spec.OpenAPI30Spec()
	if err != nil {
		panic(fmt.Sprintf("reading openapi 3.0 spec: %v", err))
	}

	openapiYAML, err = spec.OpenAPIYAML()
	if err != nil {
		panic(fmt.Sprintf("reading openapi yaml: %v", err))
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

func formatUUID(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// NewHandler constructs an http.Handler with all unauthenticated base routes,
// spec/docs endpoints, schemas, and the strict-server Conductor v2 REST API routes.
func NewHandler(db Pinger, server *Server) http.Handler {
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

	if server != nil {
		strictHandler := gen.NewStrictHandlerWithOptions(server, nil, gen.StrictHTTPServerOptions{
			RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
				problem.Write(w, &problem.Problem{
					Type:   "about:blank",
					Title:  "Bad Request",
					Status: http.StatusBadRequest,
					Detail: err.Error(),
				})
			},
			ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
				problem.Write(w, &problem.Problem{
					Type:   "about:blank",
					Title:  "Internal Server Error",
					Status: http.StatusInternalServerError,
					Detail: err.Error(),
				})
			},
		})
		gen.HandlerWithOptions(strictHandler, gen.StdHTTPServerOptions{
			BaseRouter: mux,
			Middlewares: []gen.MiddlewareFunc{
				AuthMiddleware(server),
			},
		})
	}

	return mux
}
