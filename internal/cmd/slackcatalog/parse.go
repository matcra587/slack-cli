package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/matcra587/slack-cli/internal/slackcatalog"
)

func parseMethod(raw []byte) (slackcatalog.Method, error) {
	method := slackcatalog.Method{SourceHash: hash(raw), Source: "official", Pagination: "none"}
	text := string(raw)
	if !strings.HasPrefix(text, "---\n") {
		return method, errors.New("missing Markdown frontmatter")
	}
	front, body, ok := strings.Cut(text[4:], "\n---\n")
	if !ok {
		return method, errors.New("unterminated Markdown frontmatter")
	}
	for line := range strings.SplitSeq(front, "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			return method, fmt.Errorf("invalid frontmatter line %q", line)
		}
		if strings.HasPrefix(line, "  ") {
			var names []string
			if err := json.Unmarshal([]byte(strings.TrimSpace(value)), &names); err != nil {
				return method, fmt.Errorf("unsupported scope list: %w", err)
			}
			if method.Scopes == nil {
				method.Scopes = make(map[string][]string)
			}
			method.Scopes[strings.TrimSpace(key)] = names
			continue
		}
		if key == "scopes" {
			continue
		}
		var scalar string
		if err := json.Unmarshal([]byte(strings.TrimSpace(value)), &scalar); err != nil {
			return method, fmt.Errorf("unsupported frontmatter scalar %s: %w", key, err)
		}
		switch key {
		case "method_name":
			method.Name = scalar
		case "summary":
			method.Description = scalar
		case "url":
			method.URL = scalar
		case "http_method":
			method.HTTPMethod = scalar
		case "rate_limit":
			method.RateLimit = scalar
		}
	}
	validName := regexp.MustCompile(`^[a-z][a-zA-Z0-9]*(?:\.[a-zA-Z][a-zA-Z0-9]*)+$`).MatchString(method.Name)
	if !validName || method.Description == "" || method.URL != "https://docs.slack.dev/reference/methods/"+method.Name {
		return method, errors.New("missing or inconsistent method name, description, or source URL")
	}
	if method.HTTPMethod == "" {
		pattern := regexp.MustCompile(`(?m)^(GET|POST) https://slack\.com/api/` + regexp.QuoteMeta(method.Name) + `$`)
		if matches := pattern.FindStringSubmatch(body); len(matches) == 2 {
			method.HTTPMethod = matches[1]
		}
	}
	if method.HTTPMethod != http.MethodGet && method.HTTPMethod != http.MethodPost {
		return method, errors.New("missing or unsupported HTTP method")
	}
	if _, rest, found := strings.Cut(body, "**Content types**"); found {
		section, _, _ := strings.Cut(rest, "**Rate Limits**")
		pattern := regexp.MustCompile("`(application/[a-zA-Z0-9.+-]+|multipart/form-data)`")
		for _, match := range pattern.FindAllStringSubmatch(section, -1) {
			method.ContentTypes = append(method.ContentTypes, match[1])
		}
		method.ContentTypes = slices.Compact(method.ContentTypes)
	}
	arguments, _, _ := strings.Cut(body, "## Usage info")
	arguments, _, _ = strings.Cut(arguments, "## Response")
	arguments, _, _ = strings.Cut(arguments, "## Errors")
	if _, rest, found := strings.Cut(arguments, "## Arguments"); found {
		pattern := regexp.MustCompile("(?m)^\\*\\*`([^`]+)`\\*\\*(?:`([^`]+)`)?(Required|Optional)")
		matches := pattern.FindAllStringSubmatchIndex(rest, -1)
		for i, positions := range matches {
			match := []string{rest[positions[0]:positions[1]], rest[positions[2]:positions[3]], "", rest[positions[6]:positions[7]]}
			if positions[4] >= 0 {
				match[2] = rest[positions[4]:positions[5]]
			}
			if match[1] == "token" {
				continue
			}
			argument := slackcatalog.Argument{Name: match[1], Type: match[2], Required: match[3] == "Required"}
			end := len(rest)
			if i+1 < len(matches) {
				end = matches[i+1][0]
			}
			defaultPattern := regexp.MustCompile("_Default:_ `([^`]+)`")
			if value := defaultPattern.FindStringSubmatch(rest[positions[1]:end]); len(value) == 2 {
				argument.Default = value[1]
			}
			method.Arguments = append(method.Arguments, argument)
			if argument.Required {
				method.Required = append(method.Required, argument.Name)
			}
			if argument.Name == "cursor" {
				method.Pagination = "cursor"
			} else if argument.Name == "page" && method.Pagination == "none" {
				method.Pagination = "page"
			}
		}
	}
	slackcatalog.ApplyPolicy(&method)
	if len(method.ContentTypes) == 0 && method.UnsupportedReason == "" {
		method.UnsupportedReason = "official documentation does not specify accepted request content types"
	}
	return method, nil
}
