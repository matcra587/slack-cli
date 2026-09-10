package blockkit_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/blockkit"
)

func TestRenderPlainIncludesReadableBlockText(t *testing.T) {
	plain := blockkit.RenderPlain([]blockkit.Block{
		blockkit.SectionBlock{Text: blockkit.MarkdownText("Deploy complete")},
		*blockkit.AttributionBlock(":robot_face:", "agent mode"),
		blockkit.TableBlock{Rows: [][]blockkit.TableCell{
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

func TestRenderReceivedTableCellVariants(t *testing.T) {
	var table blockkit.TableBlock
	err := json.Unmarshal([]byte(`{"type":"table","rows":[[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"name"}]}]},{"type":"raw_text","text":"plain"},{"type":"raw_number","value":12.5},{"type":"raw_number","value":3,"text":"three"},null]]}`), &table)
	if err != nil {
		t.Fatal(err)
	}
	got := blockkit.RenderMarkdown([]blockkit.Block{table})
	if !strings.Contains(got, "name\tplain\t12.5\tthree\t") {
		t.Fatalf("table cells lost: %q", got)
	}
}
