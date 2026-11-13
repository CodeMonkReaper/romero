// Package migrations embeds the SQL migration files for the ROMERO cognitive memory engine.
package migrations

import "embed"

// FS holds the embedded Goose SQL migrations.
//
//go:embed *.sql
var FS embed.FS
