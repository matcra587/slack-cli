package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestSDKInventoryTracesAliasesAndDeferredEndpoints(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source := `package slack
type Client struct{}
const endpoint = "chat.postMessage"
func (api *Client) Send() { api.sendWithContext() }
func (api *Client) sendWithContext() { request(endpoint) }
func request(path string) {}
func (api *Client) Search() { api.helper("search.messages") }
func (api *Client) helper(path string) {}
func (api *Client) GetUsersPaginated() {}
func (api *Client) SetPhoto() { api.helper("icon.png") }
`
	if err := os.WriteFile(filepath.Join(dir, "sdk.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sdk_test.go"), []byte("invalid Go that must be ignored"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := scanSDK(dir, "v0.test")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Methods) != 4 || len(got.SourceHash) != 64 {
		t.Fatalf("inventory = %#v", got)
	}
	for name, endpoint := range map[string]string{"Send": "chat.postMessage", "Search": "search.messages", "GetUsersPaginated": "users.list"} {
		index := slices.IndexFunc(got.Methods, func(method sdkMethod) bool { return method.Name == name })
		if index < 0 || !slices.Contains(got.Methods[index].Endpoints, endpoint) {
			t.Errorf("%s does not trace to %s: %#v", name, endpoint, got.Methods)
		}
	}
	for _, method := range got.Methods {
		if slices.Contains(method.Endpoints, "icon.png") {
			t.Fatal("filename was mistaken for an API endpoint")
		}
	}
}
