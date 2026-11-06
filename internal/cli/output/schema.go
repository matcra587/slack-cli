package output

// EnvelopeSchema describes the common JSON frame, independent of operation data.
func EnvelopeSchema() map[string]any {
	stringField := func() map[string]any { return map[string]any{"type": "string"} }
	return map[string]any{
		"type":                 "object",
		"required":             []string{"ok", "meta", "data", "errors"},
		"additionalProperties": false,
		"properties": map[string]any{
			"ok": map[string]any{"type": "boolean"},
			"meta": map[string]any{
				"type":     "object",
				"required": []string{"command", "workspace", "timestamp", "request_id", "exit_code"},
				"properties": map[string]any{
					"command": stringField(), "workspace": stringField(),
					"timestamp": stringField(), "request_id": stringField(),
					"exit_code":  map[string]any{"type": "integer"},
					"pagination": map[string]any{"type": "object"},
				},
			},
			"data": map[string]any{"type": []string{"object", "null"}},
			"errors": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object", "required": []string{"type", "message", "exit_code"},
					"properties": map[string]any{
						"type": stringField(), "message": stringField(),
						"exit_code":           map[string]any{"type": "integer"},
						"retry_after_seconds": map[string]any{"type": "integer"},
						"details":             map[string]any{"type": "object"},
					},
				},
			},
		},
	}
}
