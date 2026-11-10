// Package schema embeds the public OpenAPI snapshot the provider is built against.
package schema

import _ "embed"

// OpenAPI is the content of openapi.json, the public schema of the OnetSolutions API.
//
//go:embed openapi.json
var OpenAPI []byte
