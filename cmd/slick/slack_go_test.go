package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandsUseSlackGoDirectlyWithoutCustomSlackClientPackage(t *testing.T) {
	root := repoRootFromCommandTest(t)
	customImport := `"` + strings.Join([]string{"github.com", "matcra587", "slack-cli", "pkg", "slack"}, "/") + `"`
	customDir := filepath.Join("pkg", "slack")
	var offenders []string
	for _, dir := range []string{"cmd/slick", customDir} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			switch {
			case strings.Contains(string(raw), customImport):
				offenders = append(offenders, rel+" imports custom Slack client")
			case strings.HasPrefix(rel, customDir+string(filepath.Separator)):
				offenders = append(offenders, rel+" keeps custom Slack client package")
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", dir, err)
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("custom Slack client layer still present:\n%s", strings.Join(offenders, "\n"))
	}
}

func repoRootFromCommandTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
