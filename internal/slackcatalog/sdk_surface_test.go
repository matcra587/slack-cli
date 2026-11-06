package slackcatalog_test

import (
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"testing"

	"github.com/matcra587/slack-cli/api"
	"github.com/matcra587/slack-cli/internal/slackcatalog"
	slackgo "github.com/slack-go/slack"
)

func TestCatalogMatchesPinnedSDKSurface(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("sdk.json")
	if err != nil {
		t.Fatal(err)
	}
	var inventory struct {
		Version string `json:"version"`
		Methods []struct {
			Name string `json:"name"`
		} `json:"methods"`
	}
	if err := json.Unmarshal(raw, &inventory); err != nil {
		t.Fatal(err)
	}
	module, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	pin := regexp.MustCompile(`(?m)^\s*(?:require\s+)?github\.com/slack-go/slack\s+(v[^\s]+)`).FindSubmatch(module)
	if len(pin) != 2 || inventory.Version != string(pin[1]) || inventory.Version != slackcatalog.Provenance().SDKVersion {
		t.Fatal("SDK version changed; regenerate and review the Slack catalog")
	}
	native := reflect.TypeFor[*slackgo.Client]()
	listed := make(map[string]bool, len(inventory.Methods))
	for _, method := range inventory.Methods {
		if listed[method.Name] {
			t.Errorf("SDK method listed twice: %s", method.Name)
		}
		listed[method.Name] = true
		if _, exists := native.MethodByName(method.Name); !exists {
			t.Errorf("catalog lists removed SDK method %s", method.Name)
		}
	}
	for method := range native.Methods() {
		if !listed[method.Name] {
			t.Errorf("catalog omits compiled SDK method %s", method.Name)
		}
	}
	if len(listed) != slackcatalog.Provenance().SDKClientMethods {
		t.Fatal("SDK inventory count and published catalog disagree")
	}
}

func TestPublicClientPreservesEveryNativeSDKMethod(t *testing.T) {
	t.Parallel()
	native := reflect.TypeFor[*slackgo.Client]()
	public := reflect.TypeFor[*api.Client]()
	for method := range native.Methods() {
		promoted, exists := public.MethodByName(method.Name)
		if !exists {
			t.Errorf("public client hides SDK method %s", method.Name)
			continue
		}
		if !sameMethodSignature(method.Type, promoted.Type) {
			t.Errorf("public client changes SDK method %s: got %s, want %s", method.Name, promoted.Type, method.Type)
		}
	}
}

// Receivers differ for promoted methods; their arguments and results must not.
func sameMethodSignature(native, public reflect.Type) bool {
	if native.NumIn() != public.NumIn() || native.NumOut() != public.NumOut() || native.IsVariadic() != public.IsVariadic() {
		return false
	}
	for i := 1; i < native.NumIn(); i++ {
		if native.In(i) != public.In(i) {
			return false
		}
	}
	for i := range native.NumOut() {
		if native.Out(i) != public.Out(i) {
			return false
		}
	}
	return true
}
