package migrations

import "embed"

// Files is the SQLite migration set shipped with the application.
//
//go:embed *.sql
var Files embed.FS
