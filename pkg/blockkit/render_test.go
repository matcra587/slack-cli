package blockkit_test

import (
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/pkg/blockkit"
)

func TestRenderPlainIncludesReadableBlockText(t *testing.T) {
	plain := blockkit.RenderPlain([]blockkit.Block{
		blockkit.SectionBlock{Text: blockkit.MarkdownText("Deploy complete")},
		*blockkit.AttributionBlock(":robot_face:", "agent mode"),
		blockkit.TableBlock{Rows: [][]*blockkit.RichTextBlock{
			{blockkit.RichTextCell("Service"), blockkit.RichTextCell("Status")},
			{blockkit.RichTextCell("API"), blockkit.RichTextCell("OK")},
		}},
	})

	for _, want := range []string{"Deploy complete", "Sent via slack-cli", "Service\tStatus", "API\tOK"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("RenderPlain = %q, want substring %q", plain, want)
		}
	}
}

func TestRenderMarkdownPreservesMarkdownCompatibleText(t *testing.T) {
	rendered := blockkit.RenderMarkdown([]blockkit.Block{
		blockkit.SectionBlock{Text: blockkit.MarkdownText("*Deploy* complete")},
	})

	if rendered != "*Deploy* complete\n" {
		t.Fatalf("RenderMarkdown = %q", rendered)
	}
}
