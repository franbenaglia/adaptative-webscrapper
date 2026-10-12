package application

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneDiagnostics(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	make := func(name string, age time.Duration) string {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		_ = os.WriteFile(filepath.Join(path, DiagnosticScreenshot), []byte("png"), 0o600)
		_ = os.Chtimes(path, now.Add(-age), now.Add(-age))
		return path
	}
	old, recent := make("job-viejo", 8*24*time.Hour), make("job-reciente", time.Hour)
	stray := filepath.Join(dir, "suelto.txt")
	_ = os.WriteFile(stray, []byte("x"), 0o600)
	_ = os.Chtimes(stray, now.Add(-30*24*time.Hour), now.Add(-30*24*time.Hour))

	removed, err := PruneDiagnostics(dir, 7*24*time.Hour, now)
	if err != nil || removed != 1 {
		t.Fatalf("borrados = %d, err = %v", removed, err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("la carpeta antigua debería haberse borrado")
	}
	for _, kept := range []string{recent, stray} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("%s no debía tocarse: %v", kept, err)
		}
	}
	if removed, err := PruneDiagnostics(filepath.Join(dir, "no-existe"), time.Hour, now); err != nil || removed != 0 {
		t.Errorf("directorio inexistente = %d, %v", removed, err)
	}
}
