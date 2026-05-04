package agent

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
		message = "Sent via slack-cli (" + label + ")"
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
