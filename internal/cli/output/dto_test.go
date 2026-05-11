package output

import (
	"strings"
	"testing"

	slackgo "github.com/slack-go/slack"
)

// TestSearchMessageFromSlack_PreservesNonEmptyText guards against an
// over-eager fallback: when Slack does populate Text directly, we must
// not replace it with synthesized block content.
func TestSearchMessageFromSlack_PreservesNonEmptyText(t *testing.T) {
	msg := slackgo.SearchMessage{
		Text: "plain text body",
		Blocks: slackgo.Blocks{BlockSet: []slackgo.Block{
			slackgo.NewSectionBlock(
				slackgo.NewTextBlockObject("mrkdwn", "ignored", false, false),
				nil, nil,
			),
		}},
	}
	got := SearchMessageFromSlack(msg)
	if got.Text != "plain text body" {
		t.Fatalf("Text = %q, want original", got.Text)
	}
}

// TestSearchMessageFromSlack_SynthesizesFromSectionAndContext covers the
// shape slick itself sends: a section block holding the body and a
// context block holding the agent attribution.
func TestSearchMessageFromSlack_SynthesizesFromSectionAndContext(t *testing.T) {
	msg := slackgo.SearchMessage{
		Text: "",
		Blocks: slackgo.Blocks{BlockSet: []slackgo.Block{
			slackgo.NewSectionBlock(
				slackgo.NewTextBlockObject("mrkdwn", "Deploy complete", false, false),
				nil, nil,
			),
			slackgo.NewContextBlock("",
				slackgo.NewTextBlockObject("mrkdwn", ":robot_face: _Sent via slick_", false, false),
			),
		}},
	}
	got := SearchMessageFromSlack(msg)
	if !strings.Contains(got.Text, "Deploy complete") {
		t.Fatalf("Text = %q, want section body", got.Text)
	}
	if !strings.Contains(got.Text, "Sent via slick") {
		t.Fatalf("Text = %q, want context element", got.Text)
	}
}

// TestSearchMessageFromSlack_SynthesizesFromRichTextSection covers the
// shape Slack uses for human-typed messages via the composer.
func TestSearchMessageFromSlack_SynthesizesFromRichTextSection(t *testing.T) {
	msg := slackgo.SearchMessage{
		Text: "",
		Blocks: slackgo.Blocks{BlockSet: []slackgo.Block{
			&slackgo.RichTextBlock{
				Type: slackgo.MBTRichText,
				Elements: []slackgo.RichTextElement{
					&slackgo.RichTextSection{
						Type: slackgo.RTESection,
						Elements: []slackgo.RichTextSectionElement{
							&slackgo.RichTextSectionTextElement{Type: slackgo.RTSEText, Text: "Hello "},
							&slackgo.RichTextSectionUserElement{Type: slackgo.RTSEUser, UserID: "U123"},
							&slackgo.RichTextSectionTextElement{Type: slackgo.RTSEText, Text: " — check "},
							&slackgo.RichTextSectionLinkElement{Type: slackgo.RTSELink, URL: "https://example.com", Text: "the dashboard"},
							&slackgo.RichTextSectionTextElement{Type: slackgo.RTSEText, Text: " "},
							&slackgo.RichTextSectionEmojiElement{Type: slackgo.RTSEEmoji, Name: "rocket"},
						},
					},
				},
			},
		}},
	}
	got := SearchMessageFromSlack(msg)
	want := "Hello <@U123> — check the dashboard :rocket:"
	if got.Text != want {
		t.Fatalf("Text = %q, want %q", got.Text, want)
	}
}

// TestSearchMessageFromSlack_SynthesizesFromRichTextList covers the
// list variant — items render on separate lines with "- " prefixes.
func TestSearchMessageFromSlack_SynthesizesFromRichTextList(t *testing.T) {
	msg := slackgo.SearchMessage{
		Text: "",
		Blocks: slackgo.Blocks{BlockSet: []slackgo.Block{
			&slackgo.RichTextBlock{
				Type: slackgo.MBTRichText,
				Elements: []slackgo.RichTextElement{
					&slackgo.RichTextList{
						Type: slackgo.RTEList,
						Elements: []slackgo.RichTextElement{
							&slackgo.RichTextSection{
								Type: slackgo.RTESection,
								Elements: []slackgo.RichTextSectionElement{
									&slackgo.RichTextSectionTextElement{Type: slackgo.RTSEText, Text: "first"},
								},
							},
							&slackgo.RichTextSection{
								Type: slackgo.RTESection,
								Elements: []slackgo.RichTextSectionElement{
									&slackgo.RichTextSectionTextElement{Type: slackgo.RTSEText, Text: "second"},
								},
							},
						},
					},
				},
			},
		}},
	}
	got := SearchMessageFromSlack(msg)
	want := "- first\n- second"
	if got.Text != want {
		t.Fatalf("Text = %q, want %q", got.Text, want)
	}
}

// TestSearchMessageFromSlack_EmptyBlocksReturnsEmpty verifies that
// matches with no extractable text retain Text == "" (so callers can
// distinguish "no content recovered" from "content recovered").
func TestSearchMessageFromSlack_EmptyBlocksReturnsEmpty(t *testing.T) {
	msg := slackgo.SearchMessage{Text: "", Blocks: slackgo.Blocks{}}
	got := SearchMessageFromSlack(msg)
	if got.Text != "" {
		t.Fatalf("Text = %q, want empty", got.Text)
	}
}
