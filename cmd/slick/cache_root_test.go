package main

import (
	"strings"
	"testing"
)

func TestCacheCommandIsVisibleOnRoot(t *testing.T) {
	root := NewRootCommand()
	cacheCmd := findDirectChild(root, "cache")
	if cacheCmd == nil {
		t.Fatal("root command missing cache command")
	}
	if cacheCmd.Hidden {
		t.Fatal("cache command should be visible")
	}
	if got := strings.TrimSpace(cacheCmd.Short); got == "" {
		t.Fatal("cache command needs a short description")
	}
}
