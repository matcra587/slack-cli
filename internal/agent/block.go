package agent

type Block struct {
	Type     string        `json:"type"`
	Elements []TextElement `json:"elements"`
}

type TextElement struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func ContextBlock(attribution Attribution) *Block {
	if !attribution.Enabled {
		return nil
	}
	text := attribution.Message
	if text == "" {
		text = "Sent via slick (" + attribution.Label + ")"
	}
	return &Block{
		Type: "context",
		Elements: []TextElement{
			{
				Type: "mrkdwn",
				Text: attribution.Emoji + " _" + text + "_",
			},
		},
	}
}
