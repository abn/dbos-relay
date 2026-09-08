// Package spec holds the vendored Conductor OpenAPI specifications.
package spec

import (
	"embed"
	"fmt"
	"sync"

	"gopkg.in/yaml.v3"
)

// FS embeds the OpenAPI specification files.
//
//go:embed openapi.json openapi-3.0.json
var FS embed.FS

var (
	openapiOnce sync.Once
	openapi31   []byte
	openapi30   []byte
	openapiYAML []byte
	initErr     error
)

func load() {
	openapiOnce.Do(func() {
		var err error
		openapi31, err = FS.ReadFile("openapi.json")
		if err != nil {
			initErr = fmt.Errorf("reading openapi.json: %w", err)
			return
		}
		openapi30, err = FS.ReadFile("openapi-3.0.json")
		if err != nil {
			initErr = fmt.Errorf("reading openapi-3.0.json: %w", err)
			return
		}

		var parsed any
		if err := yaml.Unmarshal(openapi31, &parsed); err != nil {
			initErr = fmt.Errorf("parsing openapi.json to yaml: %w", err)
			return
		}
		openapiYAML, err = yaml.Marshal(parsed)
		if err != nil {
			initErr = fmt.Errorf("marshaling openapi.yaml: %w", err)
			return
		}
	})
}

// OpenAPISpec returns the OpenAPI 3.1 specification JSON bytes.
func OpenAPISpec() ([]byte, error) {
	load()
	if initErr != nil {
		return nil, initErr
	}
	return openapi31, nil
}

// OpenAPI30Spec returns the OpenAPI 3.0 specification JSON bytes.
func OpenAPI30Spec() ([]byte, error) {
	load()
	if initErr != nil {
		return nil, initErr
	}
	return openapi30, nil
}

// OpenAPIYAML returns the OpenAPI 3.1 specification in YAML format.
func OpenAPIYAML() ([]byte, error) {
	load()
	if initErr != nil {
		return nil, initErr
	}
	return openapiYAML, nil
}
