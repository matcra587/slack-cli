package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecondOutputFailurePreservesPriorGeneration(t *testing.T) {
	for _, firstExists := range []bool{false, true} {
		t.Run(map[bool]string{true: "replace", false: "create"}[firstExists], func(t *testing.T) {
			directory := t.TempDir()
			methods := filepath.Join(directory, "methods.json")
			coverage := filepath.Join(directory, "sdk.json")
			if firstExists {
				if err := os.WriteFile(methods, []byte("prior methods"), 0o640); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(coverage, []byte("prior coverage"), 0o600); err != nil {
				t.Fatal(err)
			}
			unrelated := filepath.Join(directory, "unrelated")
			if err := os.WriteFile(unrelated, []byte("keep"), 0o600); err != nil {
				t.Fatal(err)
			}
			renamed := false
			err := publishJSON([]jsonOutput{{methods, map[string]bool{"new": true}}, {coverage, map[string]bool{"new": true}}}, func(source, target string) error {
				if target == coverage {
					return errors.New("fixture second publication failed")
				}
				renamed = true
				return os.Rename(source, target)
			})
			if err == nil || !renamed {
				t.Fatalf("expected second output failure after first published: %v", err)
			}
			body, err := os.ReadFile(methods)
			if firstExists {
				if err != nil || string(body) != "prior methods" {
					t.Fatalf("prior methods lost: %s %v", body, err)
				}
				info, _ := os.Stat(methods)
				if info.Mode().Perm() != 0o640 {
					t.Fatal("prior file permissions changed")
				}
			} else if !os.IsNotExist(err) {
				t.Fatalf("failed generation left first output: %s %v", body, err)
			}
			body, err = os.ReadFile(coverage)
			if err != nil || string(body) != "prior coverage" {
				t.Fatalf("prior coverage lost: %s %v", body, err)
			}
			body, err = os.ReadFile(unrelated)
			if err != nil || string(body) != "keep" {
				t.Fatal("unrelated file changed")
			}
			files, err := os.ReadDir(directory)
			if err != nil {
				t.Fatal(err)
			}
			for _, file := range files {
				if strings.HasPrefix(file.Name(), ".slackcatalog-") {
					t.Fatal("temporary publication file leaked")
				}
			}
		})
	}
}

func TestPairPublicationPreflightsAllOutputs(t *testing.T) {
	for _, failure := range []string{"encoding", "directory", "same-path", "symlink"} {
		t.Run(failure, func(t *testing.T) {
			directory := t.TempDir()
			first := filepath.Join(directory, "methods.json")
			second := filepath.Join(directory, "sdk.json")
			if err := os.WriteFile(first, []byte("prior"), 0o600); err != nil {
				t.Fatal(err)
			}
			outputs := []jsonOutput{{first, map[string]bool{"new": true}}, {second, map[string]bool{"new": true}}}
			switch failure {
			case "encoding":
				outputs[1].value = make(chan int)
			case "directory":
				if err := os.Mkdir(second, 0o700); err != nil {
					t.Fatal(err)
				}
			case "same-path":
				outputs[1].path = first
			case "symlink":
				if err := os.Symlink(first, second); err != nil {
					t.Fatal(err)
				}
			}
			if err := publishJSON(outputs, os.Rename); err == nil {
				t.Fatal("invalid pair accepted")
			}
			body, err := os.ReadFile(first)
			if err != nil || string(body) != "prior" {
				t.Fatalf("first output changed before second validated: %s %v", body, err)
			}
		})
	}
}

func TestPairPublicationUpdatesBothOutputs(t *testing.T) {
	directory := t.TempDir()
	first, second := filepath.Join(directory, "methods.json"), filepath.Join(directory, "sdk.json")
	if err := publishJSON([]jsonOutput{{first, map[string]bool{"methods": true}}, {second, map[string]bool{"sdk": true}}}, os.Rename); err != nil {
		t.Fatal(err)
	}
	for path, expected := range map[string]string{first: `"methods": true`, second: `"sdk": true`} {
		body, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(body), expected) {
			t.Fatalf("published output missing: %s %v", body, err)
		}
	}
}
