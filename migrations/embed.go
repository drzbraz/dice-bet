// Package migrations embeds the SQL migration files so they ship inside
// the compiled binary and can be applied without any external file access.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
