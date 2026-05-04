package blockkit

import slackgo "github.com/slack-go/slack"

func AttributionBlock(emoji string, label string) *ContextBlock {
	return AttributionBlockWithMessage(emoji, "Sent via slack-cli ("+label+")")
}

func AttributionBlockWithMessage(emoji string, message string) *ContextBlock {
	return slackgo.NewContextBlock("", MarkdownText(emoji+" _"+message+"_"))
}
