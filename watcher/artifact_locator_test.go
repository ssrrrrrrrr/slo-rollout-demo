package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestArtifactLocatorPrefersLatestThenNewestFallback(t *testing.T) {
	directory := t.TempDir()
	locator := NewArtifactLocator(directory)
	if err := os.WriteFile(filepath.Join(directory, "report-one.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "report-two.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Minute)
	if err := os.Chtimes(filepath.Join(directory, "report-two.json"), future, future); err != nil {
		t.Fatal(err)
	}
	path, _, ok := locator.Resolve([]string{"report-latest.json"}, "report-*.json")
	if !ok || filepath.Base(path) != "report-two.json" {
		t.Fatalf("expected newest non-latest fallback, got %q", path)
	}
	if err := os.WriteFile(filepath.Join(directory, "report-latest.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	path, _, ok = locator.Resolve([]string{"report-latest.json"}, "report-*.json")
	if !ok || filepath.Base(path) != "report-latest.json" {
		t.Fatalf("expected explicit latest candidate, got %q", path)
	}
}

func TestArtifactLocatorRejectsTraversal(t *testing.T) {
	path, _, ok := NewArtifactLocator(t.TempDir()).Resolve([]string{"../outside.json"}, "")
	if ok || path != "" {
		t.Fatalf("traversal candidate must not resolve: %q", path)
	}
}
