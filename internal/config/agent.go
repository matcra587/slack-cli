package config

const (
	defaultAgentLabel = "agent mode"
	defaultAgentEmoji = ":robot_face:"
)

type AgentSettings struct {
	Attribution bool
	Label       string
	Emoji       string
	Message     string
}

func (w WorkspaceProfile) AgentSettings() AgentSettings {
	attribution := true
	if w.AgentAttribution != nil {
		attribution = *w.AgentAttribution
	}

	label := w.AgentLabel
	if w.Attribution.Label != "" {
		label = w.Attribution.Label
	}
	if label == "" {
		label = defaultAgentLabel
	}

	emoji := w.AgentEmoji
	if w.Attribution.Emoji != "" {
		emoji = w.Attribution.Emoji
	}
	if emoji == "" {
		emoji = defaultAgentEmoji
	}

	message := w.AgentMessage
	if w.Attribution.Message != "" {
		message = w.Attribution.Message
	}
	if message == "" {
		message = "Sent via slack-cli (" + label + ")"
	}

	return AgentSettings{
		Attribution: attribution,
		Label:       label,
		Emoji:       emoji,
		Message:     message,
	}
}
