package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/abn/relay/internal/auth"
	storegen "github.com/abn/relay/internal/store/gen"
)

type auditResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *auditResponseWriter) WriteHeader(code int) {
	if w.statusCode == 0 {
		w.statusCode = code
		w.ResponseWriter.WriteHeader(code)
	}
}

func (w *auditResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func getRealIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	return r.RemoteAddr
}

func AuditMiddleware(server *Server) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if server == nil || server.store == nil {
				next.ServeHTTP(w, r)
				return
			}

			aw := &auditResponseWriter{
				ResponseWriter: w,
				statusCode:     0,
			}

			next.ServeHTTP(aw, r)

			statusCode := aw.statusCode
			if statusCode == 0 {
				statusCode = http.StatusOK
			}

			ident, ok := auth.IdentityFromContext(r.Context())
			if !ok {
				return
			}
			if !ident.OrgID.Valid {
				return
			}

			status := "success"
			if statusCode >= 400 {
				status = "failure"
			}

			details, _ := json.Marshal(map[string]interface{}{
				"status":     status,
				"resource":   r.URL.Path,
				"ip_address": getRealIP(r),
				"subject":    ident.Subject,
			})

			_, _ = server.store.CreateAuditLog(r.Context(), storegen.CreateAuditLogParams{
				OrganisationID: ident.OrgID,
				Username:       ident.Username,
				Action:         r.Method + " " + r.URL.Path,
				Details:        details,
			})
		})
	}
}
