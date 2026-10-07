package main

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/RayleaBot/subscription-hub/internal/testkit"
)

func TestPlatformDependenciesKeepSharedRuntimeIndependent(t *testing.T) {
	root := testkit.RepositoryPath(t, "internal")
	const module = "github.com/RayleaBot/subscription-hub/internal/"
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		parts := strings.Split(relative, "/")
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if strings.HasPrefix(imported, module+"platforms/") {
				owner := strings.Split(strings.TrimPrefix(imported, module+"platforms/"), "/")[0]
				if parts[0] == "plugin" || (parts[0] == "platforms" && owner != parts[1]) {
					t.Errorf("%s imports platform implementation %s", relative, imported)
				}
			}
			// httpaction is the shared request primitive that testkit fakes, like the SDK.
			if parts[0] == "testkit" && strings.HasPrefix(imported, module) && imported != module+"httpaction" {
				t.Errorf("shared testkit depends on plugin implementation: %s -> %s", relative, imported)
			}
			if !strings.HasSuffix(path, "_test.go") && parts[0] != "testkit" && strings.HasPrefix(imported, module+"testkit") {
				t.Errorf("production code imports testkit: %s", relative)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
