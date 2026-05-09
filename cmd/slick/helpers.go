package main

// stringPtr returns nil for empty string, otherwise a pointer to the value.
func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// intPtr returns nil for non-positive values, otherwise a pointer to the value.
func intPtr(value int) *int {
	if value <= 0 {
		return nil
	}
	return &value
}
