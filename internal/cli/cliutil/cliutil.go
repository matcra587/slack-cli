// Package cliutil provides tiny helpers used by multiple cli command
// packages: nullable-pointer constructors for JSON optional fields,
// first-non-empty fallback resolution, and case-insensitive substring
// match against a haystack.
package cliutil

import "strings"

// StringPtr returns nil for an empty string, otherwise a pointer to the value.
// Used to omit optional string fields from JSON output.
func StringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// IntPtr returns nil for non-positive values, otherwise a pointer to the value.
// Used to omit optional int fields from JSON output.
func IntPtr(value int) *int {
	if value <= 0 {
		return nil
	}
	return &value
}

// FirstNonEmpty returns the first non-empty value from the provided list, or
// "" if every value is empty.
func FirstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// ContainsAnyFold reports whether the lowercased needle appears as a
// substring of any haystack (also lowercased). Used by channel and user
// filters that match a query against either ID or display name.
func ContainsAnyFold(needle string, haystacks ...string) bool {
	needle = strings.ToLower(needle)
	for _, h := range haystacks {
		if strings.Contains(strings.ToLower(h), needle) {
			return true
		}
	}
	return false
}
