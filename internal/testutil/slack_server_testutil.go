package testutil

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

type SlackHandler func(SlackRequest) SlackResponse

type SlackResponse struct {
	Status int
	Body   string
	Header http.Header
}

type SlackRequest struct {
	Method string
	Path   string
	Header http.Header
	Form   url.Values
	Body   []byte
}

type SlackServer struct {
	t        testing.TB
	server   *httptest.Server
	mu       sync.Mutex
	requests map[string][]SlackRequest
	handlers map[string]SlackHandler
}

func NewSlackServer(t testing.TB, handlers map[string]SlackHandler) *SlackServer {
	t.Helper()

	slackServer := &SlackServer{
		t:        t,
		requests: make(map[string][]SlackRequest),
		handlers: handlers,
	}
	slackServer.server = httptest.NewServer(http.HandlerFunc(slackServer.serveHTTP))
	return slackServer
}

func JSONResponse(body string) SlackResponse {
	return SlackResponse{
		Status: http.StatusOK,
		Body:   body,
		Header: http.Header{"Content-Type": []string{"application/json"}},
	}
}

func (s *SlackServer) BaseURL() string {
	return s.server.URL
}

func (s *SlackServer) Close() {
	s.server.Close()
}

func (s *SlackServer) Requests(method string) []SlackRequest {
	s.mu.Lock()
	defer s.mu.Unlock()

	requests := s.requests[method]
	out := make([]SlackRequest, len(requests))
	copy(out, requests)
	return out
}

func (s *SlackServer) serveHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(body))
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		r.Body = io.NopCloser(bytes.NewReader(body))
		_ = r.ParseForm()
	}

	method := strings.TrimPrefix(r.URL.Path, "/api/")
	request := SlackRequest{
		Method: r.Method,
		Path:   r.URL.Path,
		Header: r.Header.Clone(),
		Form:   cloneValues(r.Form),
		Body:   append([]byte(nil), body...),
	}

	s.mu.Lock()
	s.requests[method] = append(s.requests[method], request)
	s.mu.Unlock()

	handler, ok := s.handlers[method]
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"ok":false,"error":"method_not_found"}`))
		return
	}

	response := handler(request)
	for key, values := range response.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	if response.Status == 0 {
		response.Status = http.StatusOK
	}
	w.WriteHeader(response.Status)
	_, _ = w.Write([]byte(response.Body))
}

func cloneValues(values url.Values) url.Values {
	out := make(url.Values, len(values))
	for key, value := range values {
		out[key] = append([]string(nil), value...)
	}
	return out
}

var ErrSecretNotFound = errors.New("secret not found")

type FakeKeychain struct {
	mu      sync.Mutex
	secrets map[string]string
}

func NewFakeKeychain() *FakeKeychain {
	return &FakeKeychain{secrets: make(map[string]string)}
}

func (k *FakeKeychain) Set(service, user, secret string) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	k.secrets[service+"\x00"+user] = secret
	return nil
}

func (k *FakeKeychain) Get(service, user string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	secret, ok := k.secrets[service+"\x00"+user]
	if !ok {
		return "", ErrSecretNotFound
	}
	return secret, nil
}

func (k *FakeKeychain) Delete(service, user string) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	key := service + "\x00" + user
	if _, ok := k.secrets[key]; !ok {
		return ErrSecretNotFound
	}
	delete(k.secrets, key)
	return nil
}
