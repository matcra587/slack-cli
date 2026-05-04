package blockkit

import slackgo "github.com/slack-go/slack"

type Block = slackgo.Block
type TextObject = slackgo.TextBlockObject
type SectionBlock = slackgo.SectionBlock
type ContextBlock = slackgo.ContextBlock
type DividerBlock = slackgo.DividerBlock
type ImageBlock = slackgo.ImageBlock
type FileBlock = slackgo.FileBlock
type TableBlock = slackgo.TableBlock
type RichTextBlock = slackgo.RichTextBlock
type RichTextSection = slackgo.RichTextSection
type RichTextSectionTextElement = slackgo.RichTextSectionTextElement

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
