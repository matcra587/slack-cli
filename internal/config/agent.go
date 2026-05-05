package config

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
	if w.Attribution.Enabled != nil {
		attribution = *w.Attribution.Enabled
	}

	label := w.AgentLabel
	if w.Attribution.Label != "" {
		label = w.Attribution.Label
	}

	emoji := w.AgentEmoji
	if w.Attribution.Emoji != "" {
		emoji = w.Attribution.Emoji
	}

	message := w.AgentMessage
	if w.Attribution.Message != "" {
		message = w.Attribution.Message
	}

	return AgentSettings{
		Attribution: attribution,
		Label:       label,
		Emoji:       emoji,
		Message:     message,
	}
}
