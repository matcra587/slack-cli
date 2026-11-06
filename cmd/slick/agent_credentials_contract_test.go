package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func TestAgentDiscoveryDoesNotResolveCredentials(t *testing.T) {
	for _, args := range [][]string{
		{"agent", "schema", "--output=json"},
		{"agent", "schema", "--compact", "--output=json"},
		{"agent", "schema", "--shapes", "--output=json"},
		{"agent", "schema", "--path=lookup channel", "--output=json"},
		{"agent", "schema", "--path=message send", "--output=json"},
	} {
		t.Run(strings.Join(args[2:], "/"), func(t *testing.T) {
			cfg := workspaceConfig(config.TokenTypeUser)
			before, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			var lookups atomic.Int32
			var out, diagnostic bytes.Buffer
			root, runtime := NewRootCommandWithRuntime(WithConfig(cfg), WithTTY(false), WithSlackBaseURL("http://127.0.0.1:1"), WithIO(strings.NewReader(""), &out, &diagnostic), WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
				lookups.Add(1)
				return "", errors.New("synthetic-schema-credential-canary")
			})))
			root.SetArgs(append(append([]string{}, args...), "--timeout=250ms"))
			err = root.ExecuteContext(t.Context())
			if runtime.CancelTimeout != nil {
				runtime.CancelTimeout()
			}
			if err != nil || lookups.Load() != 0 || diagnostic.Len() != 0 || !strings.Contains(out.String(), `"ok":true`) || strings.Contains(out.String(), "synthetic-schema-credential-canary") {
				t.Fatal("agent discovery consulted credentials or lost its result contract")
			}
			after, marshalErr := json.Marshal(cfg)
			if marshalErr != nil || !bytes.Equal(before, after) {
				t.Fatal("agent discovery changed workspace settings")
			}
		})
	}
}
