package agent

import "strings"

type Attribution struct {
	Enabled  bool
	Category Category
	Label    string
	Emoji    string
	Message  string
}

func NewAttribution(detection Detection, opts Options) Attribution {
	if !detection.Active {
		return Attribution{}
	}

	label, emoji := defaultsForCategory(detection.Category)
	if opts.Label != "" {
		label = opts.Label
	}
	if opts.Emoji != "" {
		emoji = opts.Emoji
	}
	message := opts.Message
	if message == "" {
		message = defaultMessageForCategory(detection.Category, label)
	} else if shouldAppendAgentMode(detection, message) {
		message += " (agent mode)"
	}

	return Attribution{
		Enabled:  true,
		Category: detection.Category,
		Label:    label,
		Emoji:    emoji,
		Message:  message,
	}
}

func defaultsForCategory(category Category) (string, string) {
	switch category {
	case CategoryCLI:
		return "slack-cli", ":robot_face:"
	case CategoryAI:
		return "agent mode", ":robot_face:"
	case CategoryCI:
		return "CI/CD pipeline", ":gear:"
	case CategoryCron:
		return "cron job", ":clock1:"
	default:
		return "automation", ":wrench:"
	}
}

func defaultMessageForCategory(category Category, label string) string {
	if category == CategoryCLI {
		return "Sent via slack-cli"
	}
	return "Sent via slack-cli (" + label + ")"
}

func shouldAppendAgentMode(detection Detection, message string) bool {
	return detection.Category == CategoryAI && !strings.Contains(strings.ToLower(message), "(agent mode)")
}
