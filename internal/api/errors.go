package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/abn/relay/internal/api/gen"
	"github.com/abn/relay/internal/router"
)

// MakeErrorModel constructs a standard RFC 9457 ErrorModel.
func MakeErrorModel(status int, title, detail string) gen.ErrorModel {
	if status == http.StatusInternalServerError {
		detail = "internal server error"
	} else if strings.Contains(detail, "no rows") || strings.Contains(detail, "SQLSTATE") || strings.Contains(detail, "pgx") || strings.Contains(detail, "postgres://") || strings.Contains(detail, "failed to connect") || strings.Contains(detail, "user=") || strings.Contains(detail, "dial tcp") {
		detail = title
	}
	status64 := int64(status)
	typ := "about:blank"
	return gen.ErrorModel{
		Type:   &typ,
		Title:  &title,
		Status: &status64,
		Detail: &detail,
	}
}

// RouterErrorToModel maps errors returned by router.Router to HTTP status code and ErrorModel.
func RouterErrorToModel(err error) (int, gen.ErrorModel) {
	switch {
	case errors.Is(err, router.ErrOrgNotFound):
		return http.StatusNotFound, MakeErrorModel(http.StatusNotFound, "Organisation not found", err.Error())
	case errors.Is(err, router.ErrAppNotFound):
		return http.StatusNotFound, MakeErrorModel(http.StatusNotFound, "Application not found", err.Error())
	case errors.Is(err, router.ErrOperationUnsupported):
		return http.StatusNotImplemented, MakeErrorModel(http.StatusNotImplemented, "Not Implemented", err.Error())
	case errors.Is(err, router.ErrNoLiveExecutor):
		return http.StatusServiceUnavailable, MakeErrorModel(http.StatusServiceUnavailable, "Service Unavailable", err.Error())
	case errors.Is(err, router.ErrExecutorTimeout):
		return http.StatusGatewayTimeout, MakeErrorModel(http.StatusGatewayTimeout, "Gateway Timeout", err.Error())
	case errors.Is(err, router.ErrExecutorError):
		return http.StatusBadRequest, MakeErrorModel(http.StatusBadRequest, "Executor Error", err.Error())
	case errors.Is(err, router.ErrStoreUnavailable):
		return http.StatusServiceUnavailable, MakeErrorModel(http.StatusServiceUnavailable, "Service Unavailable", "database store is unavailable")
	case errors.Is(err, router.ErrDataPlaneUnavailable):
		return http.StatusServiceUnavailable, MakeErrorModel(http.StatusServiceUnavailable, "Service Unavailable", "data-plane unavailable")
	case errors.Is(err, router.ErrReadOnlyMode):
		return http.StatusForbidden, MakeErrorModel(http.StatusForbidden, "Forbidden", err.Error())
	default:
		return http.StatusInternalServerError, MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", err.Error())
	}
}
