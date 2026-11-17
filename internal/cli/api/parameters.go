package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"
	"strings"

	xfilepath "github.com/gechr/x/filepath"
)

const maxInputBytes = 4 << 20

func readParameters(input string, fields []string, stdin io.Reader) (map[string]any, error) {
	params := make(map[string]any)
	if input != "" {
		reader := stdin
		if input != "-" {
			file, err := os.Open(xfilepath.Expand(input))
			if err != nil {
				return nil, errors.New("cannot open API input file")
			}
			defer func() { _ = file.Close() }()
			reader = file
		}
		raw, err := io.ReadAll(io.LimitReader(reader, maxInputBytes+1))
		if err != nil || len(raw) > maxInputBytes {
			return nil, errors.New("API input must be readable and no larger than 4 MiB")
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&params); err != nil || params == nil {
			return nil, errors.New("API input must be a JSON object")
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			return nil, errors.New("API input must contain exactly one JSON object")
		}
	}
	for _, field := range fields {
		key, value, found := strings.Cut(field, "=")
		if !found || strings.TrimSpace(key) == "" {
			return nil, errors.New("each --field must have a nonempty name followed by =value")
		}
		params[key] = value
	}
	if containsCredential(params) {
		return nil, errors.New("credential parameters are forbidden; use the selected workspace credentials")
	}
	return params, nil
}

func containsCredential(value any) bool {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			normalized := strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(key))
			switch normalized {
			case "token", "accesstoken", "refreshtoken", "clientsecret", "password", "authorization", "botaccesstoken", "useraccesstoken", "apptoken", "apikey", "privatekey", "secret":
				return true
			}
			if strings.HasSuffix(normalized, "password") || strings.HasSuffix(normalized, "secret") || strings.HasSuffix(normalized, "token") {
				return true
			}
			if containsCredential(child) {
				return true
			}
		}
	case []any:
		return slices.ContainsFunc(value, containsCredential)
	}
	return false
}
