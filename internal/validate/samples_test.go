package validate

import (
	"os"
	"path/filepath"
	"testing"
)

func mustSample(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("samples", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
