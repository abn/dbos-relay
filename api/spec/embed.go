// Package spec holds the vendored Conductor OpenAPI specifications.
package spec

import "embed"

// FS embeds the OpenAPI specification files.
//
//go:embed openapi.json openapi-3.0.json
var FS embed.FS
