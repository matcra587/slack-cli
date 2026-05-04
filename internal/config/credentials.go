package config

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/zalando/go-keyring"
)

var ErrCredentialNotFound = errors.New("credential not found")

type CredentialPayload struct {
	AccessToken  string     `json:"access_token"`
	RefreshToken string     `json:"refresh_token,omitempty"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	ClientID     string     `json:"client_id,omitempty"`
}

func EncodeCredential(payload CredentialPayload) (string, error) {
	if strings.TrimSpace(payload.AccessToken) == "" {
		return "", errors.New("access_token is required")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func DecodeCredential(secret string) (CredentialPayload, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return CredentialPayload{}, errors.New("structured credential payload is required")
	}
	if !strings.HasPrefix(secret, "{") {
		if strings.HasPrefix(secret, "xox") {
			return CredentialPayload{}, errors.New("legacy plaintext credential found; structured credential payload is required; run slack auth login to replace it")
		}
		return CredentialPayload{}, errors.New("structured credential payload is required")
	}
	var payload CredentialPayload
	if err := json.Unmarshal([]byte(secret), &payload); err != nil {
		return CredentialPayload{}, err
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		return CredentialPayload{}, errors.New("access_token is required")
	}
	return payload, nil
}

type CredentialStore interface {
	Set(service, user, secret string) error
	Get(service, user string) (string, error)
	Delete(service, user string) error
}

type KeyringCredentialStore struct{}

func NewKeyringCredentialStore() KeyringCredentialStore {
	return KeyringCredentialStore{}
}

func (KeyringCredentialStore) Set(service, user, secret string) error {
	return keyring.Set(service, user, secret)
}

func (KeyringCredentialStore) Get(service, user string) (string, error) {
	secret, err := keyring.Get(service, user)
	if err != nil {
		return "", ErrCredentialNotFound
	}
	return secret, nil
}

func (KeyringCredentialStore) Delete(service, user string) error {
	if err := keyring.Delete(service, user); err != nil {
		return ErrCredentialNotFound
	}
	return nil
}

type MemoryCredentialStore struct {
	secrets map[string]string
}

func NewMemoryCredentialStore() *MemoryCredentialStore {
	return &MemoryCredentialStore{secrets: make(map[string]string)}
}

func (s *MemoryCredentialStore) Set(service, user, secret string) error {
	s.secrets[service+"\x00"+user] = secret
	return nil
}

func (s *MemoryCredentialStore) Get(service, user string) (string, error) {
	secret, ok := s.secrets[service+"\x00"+user]
	if !ok {
		return "", ErrCredentialNotFound
	}
	return secret, nil
}

func (s *MemoryCredentialStore) Delete(service, user string) error {
	key := service + "\x00" + user
	if _, ok := s.secrets[key]; !ok {
		return ErrCredentialNotFound
	}
	delete(s.secrets, key)
	return nil
}
