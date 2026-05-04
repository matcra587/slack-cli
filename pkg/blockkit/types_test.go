package blockkit_test

import (
	"encoding/json"
	"testing"

	"github.com/matcra587/slack-cli/pkg/blockkit"
)

func TestBlockKitTypesMarshalToSlackNativeJSON(t *testing.T) {
	blocks := []blockkit.Block{
		blockkit.SectionBlock{Type: "section", Text: blockkit.MarkdownText("hello")},
		*blockkit.AttributionBlock("", "context"),
		blockkit.DividerBlock{Type: "divider"},
		blockkit.ImageBlock{Type: "image", ImageURL: "https://example.com/image.png", AltText: "example"},
		blockkit.FileBlock{Type: "file", ExternalID: "F123", Source: "remote"},
		blockkit.TableBlock{Type: "table", Rows: [][]*blockkit.RichTextBlock{{blockkit.RichTextCell("service"), blockkit.RichTextCell("status")}}},
	}

	raw, err := json.Marshal(blocks)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	var decoded []map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal returned error: %v\n%s", err, raw)
	}
	wantTypes := []string{"section", "context", "divider", "image", "file", "table"}
	for i, want := range wantTypes {
		if decoded[i]["type"] != want {
			t.Fatalf("block %d type = %q, want %q", i, decoded[i]["type"], want)
		}
	}
}

func TestTextObjectPreservesMarkdownAndPlainTextTypes(t *testing.T) {
	markdown := blockkit.MarkdownText("*deploy*")
	plain := blockkit.PlainText("deploy")

	if markdown.Type != blockkit.TextTypeMarkdown {
		t.Fatalf("markdown Type = %q", markdown.Type)
	}
	if plain.Type != blockkit.TextTypePlain {
		t.Fatalf("plain Type = %q", plain.Type)
	}
}
