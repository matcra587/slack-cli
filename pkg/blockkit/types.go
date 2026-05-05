package blockkit

import slackgo "github.com/slack-go/slack"

type (
	Block                      = slackgo.Block
	TextObject                 = slackgo.TextBlockObject
	SectionBlock               = slackgo.SectionBlock
	ContextBlock               = slackgo.ContextBlock
	DividerBlock               = slackgo.DividerBlock
	ImageBlock                 = slackgo.ImageBlock
	FileBlock                  = slackgo.FileBlock
	TableBlock                 = slackgo.TableBlock
	RichTextBlock              = slackgo.RichTextBlock
	RichTextSection            = slackgo.RichTextSection
	RichTextSectionTextElement = slackgo.RichTextSectionTextElement
)

const (
	TextTypeMarkdown = "mrkdwn"
	TextTypePlain    = "plain_text"
)

func MarkdownText(text string) *TextObject {
	return slackgo.NewTextBlockObject(TextTypeMarkdown, text, false, false)
}

func PlainText(text string) *TextObject {
	return slackgo.NewTextBlockObject(TextTypePlain, text, false, false)
}

func RichTextCell(text string) *slackgo.RichTextBlock {
	return slackgo.NewRichTextBlock("", slackgo.NewRichTextSection(slackgo.NewRichTextSectionTextElement(text, nil)))
}
