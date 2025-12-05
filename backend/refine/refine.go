// Package refine provides the public entrypoint for the internal refinement service.
// It simply re-exports the vetted backend/internal/refine implementation so that
// packages outside backend/ (main.go) can construct a Service without depending on
// the internal package directly.
package refine

import (
	"net/http"

	"alert_framework/backend/internal/refine"
	"alert_framework/config"
)

// Service re-exports the internal Service type.
type Service = refine.Service

// Request re-exports the internal Request type.
type Request = refine.Request

// NewService re-exports the internal constructor.
func NewService(client *http.Client, cfg config.Config) (*refine.Service, error) {
	return refine.NewService(client, cfg)
}
