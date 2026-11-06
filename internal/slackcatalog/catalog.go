// Package slackcatalog records Slack's published Web API request contracts.
// A catalog entry describes an endpoint; it does not prove live compatibility.
package slackcatalog

import (
	_ "embed"
	"encoding/json"
	"strings"
	"sync"
)

// Argument describes a published method parameter. Required excludes the token,
// which clients supply through the Authorization header.
type Argument struct {
	Name     string `json:"name"`
	Type     string `json:"type,omitempty"`
	Required bool   `json:"required,omitempty"`
	Default  string `json:"default,omitempty"`
}

// Method combines published facts with slick's explicit execution policy.
type Method struct {
	Name              string              `json:"name"`
	Description       string              `json:"description"`
	URL               string              `json:"url"`
	HTTPMethod        string              `json:"http_method,omitempty"`
	RateLimit         string              `json:"rate_limit,omitempty"`
	Scopes            map[string][]string `json:"scopes,omitempty"`
	Required          []string            `json:"required,omitempty"`
	Arguments         []Argument          `json:"arguments,omitempty"`
	ReadOnly          bool                `json:"read_only"`
	Pagination        string              `json:"pagination"`
	CursorInitial     string              `json:"cursor_initial,omitempty"`
	ContentTypes      []string            `json:"content_types,omitempty"`
	Authentication    string              `json:"authentication"`
	ResponseFormat    string              `json:"response_format"`
	Deprecated        bool                `json:"deprecated,omitempty"`
	SensitiveResponse bool                `json:"sensitive_response,omitempty"`
	UnsupportedReason string              `json:"unsupported_reason,omitempty"`
	SourceHash        string              `json:"source_hash"`
	Source            string              `json:"source"`
	SDKMethods        []string            `json:"sdk_methods,omitempty"`
}

// Snapshot records the source inputs and their generation date.
type Snapshot struct {
	GeneratedAt      string   `json:"generated_at"`
	IndexURL         string   `json:"index_url"`
	IndexHash        string   `json:"index_hash"`
	SDKVersion       string   `json:"sdk_version"`
	OfficialMethods  int      `json:"official_methods"`
	SDKClientMethods int      `json:"sdk_client_methods"`
	Methods          []Method `json:"methods"`
}

//go:embed methods.json
var source []byte

var snapshot = sync.OnceValue(func() Snapshot {
	var value Snapshot
	if err := json.Unmarshal(source, &value); err != nil {
		panic("invalid embedded Slack method catalog: " + err.Error())
	}
	return value
})

// Lookup accepts a canonical method name or the lowercase documentation spelling.
// It returns a copy, so callers cannot change future policy decisions.
func Lookup(name string) (Method, bool) {
	for _, method := range snapshot().Methods {
		if strings.EqualFold(method.Name, name) {
			return clone(method), true
		}
	}
	return Method{}, false
}

// All returns the complete catalog in canonical name order.
func All() []Method {
	methods := make([]Method, len(snapshot().Methods))
	for i, method := range snapshot().Methods {
		methods[i] = clone(method)
	}
	return methods
}

// Provenance returns generation details without loading all method entries.
func Provenance() Snapshot {
	value := snapshot()
	value.Methods = nil
	return value
}

func clone(value Method) Method {
	value.Required = append([]string(nil), value.Required...)
	value.Arguments = append([]Argument(nil), value.Arguments...)
	value.ContentTypes = append([]string(nil), value.ContentTypes...)
	value.SDKMethods = append([]string(nil), value.SDKMethods...)
	if value.Scopes != nil {
		scopes := make(map[string][]string, len(value.Scopes))
		for kind, names := range value.Scopes {
			scopes[kind] = append([]string(nil), names...)
		}
		value.Scopes = scopes
	}
	return value
}
