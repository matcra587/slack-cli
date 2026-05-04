package config_test

import (
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func TestCredentialPayloadRoundTripsWithoutStandaloneRawToken(t *testing.T) {
	encoded, err := config.EncodeCredential(config.CredentialPayload{AccessToken: "xoxb-secret", ClientID: "C123"})
	if err != nil {
		t.Fatalf("EncodeCredential returned error: %v", err)
	}
	if encoded == "xoxb-secret" {
		t.Fatal("credential payload stored raw token as the entire secret")
	}
	if !strings.Contains(encoded, `"access_token"`) {
		t.Fatalf("encoded credential = %q, want structured payload", encoded)
	}
	if !strings.Contains(encoded, `"client_id"`) {
		t.Fatalf("encoded credential = %q, want OAuth client id metadata", encoded)
	}

	decoded, err := config.DecodeCredential(encoded)
	if err != nil {
		t.Fatalf("DecodeCredential returned error: %v", err)
	}
	if decoded.AccessToken != "xoxb-secret" {
		t.Fatalf("AccessToken = %q, want xoxb-secret", decoded.AccessToken)
	}
	if decoded.ClientID != "C123" {
		t.Fatalf("ClientID = %q, want C123", decoded.ClientID)
	}
}

func TestDecodeCredentialRejectsLegacyRawTokenSecret(t *testing.T) {
	if _, err := config.DecodeCredential("xoxb-secret"); err == nil {
		t.Fatal("DecodeCredential accepted raw token secret")
	}
}
