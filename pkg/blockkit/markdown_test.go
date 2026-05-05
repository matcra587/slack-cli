package blockkit_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/pkg/blockkit"
)

func TestFromMarkdownConvertsParagraphToSection(t *testing.T) {
	blocks, err := blockkit.FromMarkdown("Deploy *complete*")
	if err != nil {
		t.Fatalf("FromMarkdown returned error: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("block count = %d, want 1", len(blocks))
	}
	section, ok := blocks[0].(*blockkit.SectionBlock)
	if !ok {
		t.Fatalf("block type = %T, want SectionBlock", blocks[0])
	}
	if section.Text == nil {
		t.Fatal("section text is nil")
	}
	if section.Text.Text != "Deploy complete" {
		t.Fatalf("section text = %q, want stripped Markdown text", section.Text.Text)
	}
}

func TestFromMarkdownPreservesUnsupportedBlockSourceFallback(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "list",
			input: "- alpha\n- beta\n",
			want:  "- alpha\n- beta",
		},
		{
			name:  "blockquote",
			input: "> quoted\n> source\n",
			want:  "> quoted\n> source",
		},
		{
			name:  "fenced code",
			input: "```sh\necho hello\n```\n",
			want:  "```sh\necho hello\n```",
		},
		{
			name:  "raw html",
			input: "<details>\n<summary>Deploy</summary>\n</details>\n",
			want:  "<details>\n<summary>Deploy</summary>\n</details>",
		},
		{
			name:  "unsupported extension marker",
			input: "~~obsolete~~\n",
			want:  "~~obsolete~~",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocks, err := blockkit.FromMarkdown(tt.input)
			if err != nil {
				t.Fatalf("FromMarkdown returned error: %v", err)
			}
			if len(blocks) != 1 {
				t.Fatalf("block count = %d, want 1", len(blocks))
			}
			section, ok := blocks[0].(*blockkit.SectionBlock)
			if !ok {
				t.Fatalf("block type = %T, want SectionBlock", blocks[0])
			}
			if section.Text == nil {
				t.Fatal("section text is nil")
			}
			if got := strings.TrimSpace(section.Text.Text); got != tt.want {
				t.Fatalf("section text = %q, want source-preserving fallback %q", got, tt.want)
			}
		})
	}
}

func TestFromMarkdownConvertsTableToSlackTableBlock(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "blockkit", "markdown_table.md"))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	blocks, err := blockkit.FromMarkdown(string(raw))
	if err != nil {
		t.Fatalf("FromMarkdown returned error: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("block count = %d, want 1", len(blocks))
	}

	table, ok := blocks[0].(blockkit.TableBlock)
	if !ok {
		t.Fatalf("block type = %T, want TableBlock", blocks[0])
	}
	if len(table.Rows) != 3 {
		t.Fatalf("table rows = %d, want header plus two body rows", len(table.Rows))
	}
	if blockkit.RenderPlain([]blockkit.Block{table}) != "Service\tStatus\nAPI\tOK\nWorker\tDegraded\n" {
		t.Fatalf("unexpected table cells: %#v", table.Rows)
	}
}
