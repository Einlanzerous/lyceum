// Package migrations embeds the Lyceum SQL migration files so they can be
// applied at runtime by the store's migrator (see internal/store).
package migrations

import "embed"

// FS holds every *.up.sql / *.down.sql migration in this directory.
//
//go:embed *.sql
var FS embed.FS
