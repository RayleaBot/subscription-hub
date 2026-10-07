package testkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func RepositoryPath(t testing.TB, relative string) string {
	t.Helper()
	path, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if data, err := os.ReadFile(filepath.Join(path, "go.mod")); err == nil && strings.Contains(string(data), "module github.com/RayleaBot/subscription-hub") {
			return filepath.Join(path, filepath.FromSlash(relative))
		}
		parent := filepath.Dir(path)
		if parent == path {
			t.Fatal("subscription hub repository root not found")
		}
		path = parent
	}
}
