// Package migrations contiene las migraciones SQLite versionadas. Se aplican
// en orden alfabético de nombre de archivo y nunca se modifican una vez
// publicadas: los cambios van en un archivo nuevo.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
