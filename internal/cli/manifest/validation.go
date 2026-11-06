package manifest

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

var manifestColorRE = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

func validateTemplateDisplay(opts templateOptions) error {
	for _, field := range []struct {
		name, value string
		min, max    int
	}{
		{"name", opts.Name, 1, 35},
		{"description", opts.Description, 0, 140},
		{"long-description", opts.LongDescription, 175, 4000},
	} {
		value := strings.TrimSpace(field.value)
		if value == "" {
			continue
		}
		count := utf8.RuneCountInString(value)
		if count < field.min || count > field.max {
			return fmt.Errorf("%s must contain between %d and %d characters", field.name, field.min, field.max)
		}
	}
	color := strings.TrimSpace(opts.BackgroundColor)
	if color != "" && !manifestColorRE.MatchString(color) {
		return errors.New("background-color must be #RGB or #RRGGBB hexadecimal")
	}
	return nil
}
