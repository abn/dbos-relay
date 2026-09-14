// Package api serves Relay's HTTP endpoints, including health checks,
// the vendored OpenAPI specifications, interactive documentation, and schemas.
package api

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

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

//go:embed swagger/swagger-ui.css
var swaggerCSS []byte

//go:embed swagger/swagger-ui-bundle.js
var swaggerJS []byte

const docsHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Relay API Documentation</title>
  <link rel="stylesheet" href="/docs/swagger-ui.css" />
</head>
<body>
<div id="swagger-ui"></div>
<script src="/docs/swagger-ui-bundle.js"></script>
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
				Detail: "database unavailable",
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

	mux.HandleFunc("/docs/swagger-ui.css", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(swaggerCSS)
	})

	mux.HandleFunc("/docs/swagger-ui-bundle.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(swaggerJS)
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
		needsAttentionHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			orgName := r.PathValue("orgName")
			appName := r.PathValue("appName")
			report, code, errModel := server.GetNeedsAttention(r.Context(), orgName, appName, 15*time.Minute)
			title := "Error"
			if errModel.Title != nil {
				title = *errModel.Title
			}
			detail := ""
			if errModel.Detail != nil {
				detail = *errModel.Detail
			}
			if code != http.StatusOK {
				problem.Write(w, &problem.Problem{
					Type:   "about:blank",
					Title:  title,
					Status: code,
					Detail: detail,
				})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(report)
		})
		mux.Handle("GET /v2/orgs/{orgName}/apps/{appName}/needs-attention", AuditMiddleware(server)(AuthMiddleware(server)(needsAttentionHandler)))

		strictHandler := gen.NewStrictHandlerWithOptions(server, nil, gen.StrictHTTPServerOptions{
			RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
				problem.Write(w, &problem.Problem{
					Type:   "about:blank",
					Title:  "Bad Request",
					Status: http.StatusBadRequest,
					Detail: "bad request",
				})
			},
			ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
				problem.Write(w, &problem.Problem{
					Type:   "about:blank",
					Title:  "Internal Server Error",
					Status: http.StatusInternalServerError,
					Detail: "internal server error",
				})
			},
		})
		gen.HandlerWithOptions(strictHandler, gen.StdHTTPServerOptions{
			BaseRouter: mux,
			Middlewares: []gen.MiddlewareFunc{
				AuditMiddleware(server),
				AuthMiddleware(server),
			},
			ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
				problem.Write(w, &problem.Problem{
					Type:   "about:blank",
					Title:  "Bad Request",
					Status: http.StatusBadRequest,
					Detail: err.Error(),
				})
			},
		})
	}

	return &problemHandler{handler: mux, server: server}
}

type problemResponseWriter struct {
	http.ResponseWriter
	statusCode int
	intercept  bool
}

func (rw *problemResponseWriter) WriteHeader(code int) {
	if rw.statusCode != 0 {
		return
	}
	ct := rw.Header().Get("Content-Type")
	if (code == http.StatusNotFound || code == http.StatusMethodNotAllowed) && !strings.Contains(ct, "problem+json") {
		rw.statusCode = code
		rw.intercept = true
		return
	}
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *problemResponseWriter) Write(b []byte) (int, error) {
	if rw.intercept {
		return len(b), nil
	}
	if rw.statusCode == 0 {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}

type problemHandler struct {
	handler http.Handler
	server  *Server
}

func isOAuthGatedPath(p string) bool {
	p = strings.TrimSuffix(p, "/")
	if p == "/v2/users" || p == "/v2/users/me" || p == "/v2/users/register" {
		return true
	}
	parts := strings.Split(p, "/")
	if len(parts) == 4 && parts[1] == "v2" && parts[2] == "orgs" && parts[3] != "" {
		return true
	}
	if len(parts) >= 5 && parts[1] == "v2" && parts[2] == "orgs" {
		switch parts[4] {
		case "join", "secret", "secrets", "members", "roles", "users", "domain-claims", "audit-logs":
			return true
		}
	}
	return false
}

func (h *problemHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.server != nil && !h.server.authEnabled && isOAuthGatedPath(r.URL.Path) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(oidcNotAvailableError())
		return
	}
	rw := &problemResponseWriter{ResponseWriter: w}
	h.handler.ServeHTTP(rw, r)
	if rw.intercept {
		var title, detail string
		if rw.statusCode == http.StatusNotFound {
			title = "Not Found"
			detail = "the requested resource was not found"
		} else {
			title = "Method Not Allowed"
			detail = "the request method is not supported for this route"
		}
		w.Header().Del("X-Content-Type-Options")
		problem.Write(w, &problem.Problem{
			Type:   "about:blank",
			Title:  title,
			Status: rw.statusCode,
			Detail: detail,
		})
	}
}
