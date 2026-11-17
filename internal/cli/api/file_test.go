package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFailedFileWriteRemovesOwnedOutputAndPreservesReplacedPath(t *testing.T) {
	for _, replaced := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), "output.json")
		file, err := createResponseFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.file.Close(); err != nil {
			t.Fatal(err)
		}
		if err := file.write([]byte(`{"fixture":"sensitive"}`)); err == nil {
			t.Fatal("closed file accepted write")
		}
		if replaced {
			// Rename preserves the original inode and prevents immediate inode
			// reuse from obscuring the replacement identity check.
			if err := os.Rename(path, path+".old"); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("unrelated"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		file.cleanup()
		body, err := os.ReadFile(path)
		if replaced {
			if err != nil || string(body) != "unrelated" {
				t.Fatalf("unrelated replacement lost: %s %v", body, err)
			}
		} else if !os.IsNotExist(err) {
			t.Fatalf("failed output retained: %s %v", body, err)
		}
	}
}
