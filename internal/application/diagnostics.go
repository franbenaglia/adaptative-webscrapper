package application

import (
	"errors"
	"os"
	"path/filepath"
	"time"
)

// PruneDiagnostics borra las carpetas de diagnóstico de trabajos (las
// subcarpetas directas de dir) que no se modifican desde hace más de maxAge.
// Devuelve cuántas borró. Un dir inexistente no es un error.
func PruneDiagnostics(dir string, maxAge time.Duration, now time.Time) (int, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	removed := 0
	var errs []error
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || !entry.IsDir() || now.Sub(info.ModTime()) <= maxAge {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			errs = append(errs, err)
			continue
		}
		removed++
	}
	return removed, errors.Join(errs...)
}
