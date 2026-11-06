package blockkit_test

import (
	"testing"

	"github.com/matcra587/slack-cli/internal/blockkit"
)

func TestMarkdownInlineSlackFormatting(t *testing.T) {
	tests := []struct{ name, input, want string }{
		{"named link", "[Slack formatting guide](https://docs.slack.dev/messaging/formatting-message-text/)", "<https://docs.slack.dev/messaging/formatting-message-text/|Slack formatting guide>"},
		{"strong", "**Ready** and __approved__", "*Ready* and *approved*"},
		{"native syntax", "*bold* _italic_ ~strike~", "*bold* _italic_ ~strike~"},
		{"strikethrough", "~~old wording~~", "~old wording~"},
		{"nested link", "**[docs](https://docs.slack.dev/)**", "*<https://docs.slack.dev/|docs>*"},
		{"reference link", "[docs][guide]\n\n[guide]: https://docs.slack.dev/", "<https://docs.slack.dev/|docs>"},
		{"query escaping", "[A & B](https://example.test/?a=1&b=2)", "<https://example.test/?a=1&amp;b=2|A &amp; B>"},
		{"code is literal", "`[docs](https://docs.slack.dev/) **bold**`", "`[docs](https://docs.slack.dev/) **bold**`"},
		{"raw sentinel remains literal", "<!channel> <https://evil.test|trusted.test>", "&lt;!channel&gt; &lt;https://evil.test|trusted.test&gt;"},
		{"unsupported scheme", "[unsafe](javascript:alert%281%29)", "[unsafe](javascript:alert%281%29)"},
		{"link label sentinel", "[<!channel>](https://example.test/)", "<https://example.test/|&lt;!channel&gt;>"},
		{"URL delimiter protection", "[docs](https://example.test/a%3Eb?x=one|two)", "<https://example.test/a%3Eb?x=one%7Ctwo|docs>"},
		{"pipe label fallback", "[one|two](https://example.test/)", "[one|two](https://example.test/)"},
		{"relative target", "[readme](../README.md)", "[readme](../README.md)"},
		{"heading", "# **Release** [docs](https://docs.slack.dev/)", "*Release* <https://docs.slack.dev/|docs>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocks, err := blockkit.FromMarkdown(tt.input)
			if err != nil {
				t.Fatal(err)
			}
			if len(blocks) != 1 {
				t.Fatalf("block count=%d, want one", len(blocks))
			}
			section, ok := blocks[0].(*blockkit.SectionBlock)
			if !ok || section.Text == nil {
				t.Fatalf("expected section, got %T", blocks[0])
			}
			if got := section.Text.Text; got != tt.want {
				t.Fatalf("rendered Slack text=%q, want %q", got, tt.want)
			}
		})
	}
}
