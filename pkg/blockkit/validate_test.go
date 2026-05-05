package blockkit_test

import (
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/pkg/blockkit"
	slackgo "github.com/slack-go/slack"
)

func TestValidateBlocksAcceptsSupportedBlocks(t *testing.T) {
	blocks := []blockkit.Block{
		blockkit.SectionBlock{Text: blockkit.MarkdownText("hello")},
		*blockkit.AttributionBlock(":robot_face:", "agent mode"),
		blockkit.TableBlock{Rows: [][]*blockkit.RichTextBlock{{blockkit.RichTextCell("service")}}},
	}

	if err := blockkit.ValidateBlocks(blocks); err != nil {
		t.Fatalf("ValidateBlocks returned error: %v", err)
	}
}

func TestValidateBlocksRejectsTooManyBlocks(t *testing.T) {
	blocks := make([]blockkit.Block, 51)
	for i := range blocks {
		blocks[i] = blockkit.DividerBlock{}
	}

	err := blockkit.ValidateBlocks(blocks)
	if err == nil || !strings.Contains(err.Error(), "50") {
		t.Fatalf("ValidateBlocks error = %v, want block count limit", err)
	}
}

func TestValidateBlocksRejectsSectionTextLimit(t *testing.T) {
	err := blockkit.ValidateBlocks([]blockkit.Block{
		blockkit.SectionBlock{Text: blockkit.MarkdownText(strings.Repeat("x", 3001))},
	})
	if err == nil || !strings.Contains(err.Error(), "section text") {
		t.Fatalf("ValidateBlocks error = %v, want section text limit", err)
	}
}

func TestValidateBlocksRejectsMissingSectionText(t *testing.T) {
	err := blockkit.ValidateBlocks([]blockkit.Block{blockkit.SectionBlock{}})
	if err == nil || !strings.Contains(err.Error(), "section text or fields are required") {
		t.Fatalf("ValidateBlocks error = %v, want missing section text validation", err)
	}
}

func TestValidateBlocksRejectsUnknownTopLevelBlockWithoutPanic(t *testing.T) {
	err := blockkit.ValidateBlocks([]blockkit.Block{fakeBlock{}})
	if err == nil || !strings.Contains(err.Error(), "unsupported type") {
		t.Fatalf("ValidateBlocks error = %v, want unsupported block type", err)
	}
}

func TestValidateBlocksRejectsBadShapesWithoutPanic(t *testing.T) {
	tests := []struct {
		name  string
		block blockkit.Block
		want  string
	}{
		{name: "nil section pointer", block: (*blockkit.SectionBlock)(nil), want: "nil"},
		{name: "nil table pointer", block: (*blockkit.TableBlock)(nil), want: "nil"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("ValidateBlocks panicked: %v", recovered)
				}
			}()
			err := blockkit.ValidateBlocks([]blockkit.Block{tt.block})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateBlocks error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestValidateBlocksRejectsTableLimits(t *testing.T) {
	t.Run("more than one table", func(t *testing.T) {
		err := blockkit.ValidateBlocks([]blockkit.Block{
			blockkit.TableBlock{Rows: [][]*blockkit.RichTextBlock{{blockkit.RichTextCell("a")}}},
			blockkit.TableBlock{Rows: [][]*blockkit.RichTextBlock{{blockkit.RichTextCell("b")}}},
		})
		if err == nil || !strings.Contains(err.Error(), "one table") {
			t.Fatalf("ValidateBlocks error = %v, want one table limit", err)
		}
	})

	t.Run("too many rows", func(t *testing.T) {
		rows := make([][]*blockkit.RichTextBlock, 101)
		for i := range rows {
			rows[i] = []*blockkit.RichTextBlock{blockkit.RichTextCell("x")}
		}
		err := blockkit.ValidateBlocks([]blockkit.Block{blockkit.TableBlock{Rows: rows}})
		if err == nil || !strings.Contains(err.Error(), "100") {
			t.Fatalf("ValidateBlocks error = %v, want row limit", err)
		}
	})

	t.Run("too many columns", func(t *testing.T) {
		row := make([]*blockkit.RichTextBlock, 21)
		for i := range row {
			row[i] = blockkit.RichTextCell("x")
		}
		err := blockkit.ValidateBlocks([]blockkit.Block{blockkit.TableBlock{Rows: [][]*blockkit.RichTextBlock{row}}})
		if err == nil || !strings.Contains(err.Error(), "20") {
			t.Fatalf("ValidateBlocks error = %v, want column limit", err)
		}
	})
}

func TestValidateRawBlocksRejectsRequiredFieldsAndLimits(t *testing.T) {
	longText := strings.Repeat("x", 3001)
	tooManyContext := make([]any, 11)
	for i := range tooManyContext {
		tooManyContext[i] = map[string]any{"type": "mrkdwn", "text": "ctx"}
	}
	tooManyBlocks := make([]map[string]any, 51)
	for i := range tooManyBlocks {
		tooManyBlocks[i] = map[string]any{"type": "divider"}
	}

	tests := []struct {
		name   string
		blocks []map[string]any
		want   string
	}{
		{
			name:   "section missing text or fields",
			blocks: []map[string]any{{"type": "section"}},
			want:   "section text or fields are required",
		},
		{
			name:   "section text exceeds limit",
			blocks: []map[string]any{{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": longText}}},
			want:   "text exceeds 3000",
		},
		{
			name:   "context elements required",
			blocks: []map[string]any{{"type": "context", "elements": []any{}}},
			want:   "context elements are required",
		},
		{
			name:   "context element count",
			blocks: []map[string]any{{"type": "context", "elements": tooManyContext}},
			want:   "context elements exceed 10",
		},
		{
			name:   "image url required",
			blocks: []map[string]any{{"type": "image", "alt_text": "diagram"}},
			want:   "image_url is required",
		},
		{
			name:   "image alt text required",
			blocks: []map[string]any{{"type": "image", "image_url": "https://example.com/image.png"}},
			want:   "alt_text is required",
		},
		{
			name:   "file fields required",
			blocks: []map[string]any{{"type": "file", "external_id": "F123"}},
			want:   "file external_id and source are required",
		},
		{
			name:   "table rows required",
			blocks: []map[string]any{{"type": "table"}},
			want:   "table rows are required",
		},
		{
			name:   "rich text elements required",
			blocks: []map[string]any{{"type": "rich_text"}},
			want:   "rich_text elements are required",
		},
		{
			name: "rich text child type required",
			blocks: []map[string]any{{
				"type":     "rich_text",
				"elements": []any{map[string]any{}},
			}},
			want: "rich_text element 0 type is required",
		},
		{
			name: "rich text section child must be object",
			blocks: []map[string]any{{
				"type": "rich_text",
				"elements": []any{map[string]any{
					"type":     "rich_text_section",
					"elements": []any{42},
				}},
			}},
			want: "rich_text_section element 0 must be an object",
		},
		{
			name: "rich text section child type required",
			blocks: []map[string]any{{
				"type": "rich_text",
				"elements": []any{map[string]any{
					"type":     "rich_text_section",
					"elements": []any{map[string]any{}},
				}},
			}},
			want: "rich_text_section element 0 type is required",
		},
		{
			name: "rich text section child unsupported type",
			blocks: []map[string]any{{
				"type": "rich_text",
				"elements": []any{map[string]any{
					"type":     "rich_text_section",
					"elements": []any{map[string]any{"type": "bogus"}},
				}},
			}},
			want: `rich_text_section element 0 unsupported type "bogus"`,
		},
		{
			name: "rich text section text child missing text",
			blocks: []map[string]any{{
				"type": "rich_text",
				"elements": []any{map[string]any{
					"type":     "rich_text_section",
					"elements": []any{map[string]any{"type": "text"}},
				}},
			}},
			want: "rich_text_section element 0 text is required",
		},
		{
			name: "rich text date timestamp must be numeric",
			blocks: []map[string]any{{
				"type": "rich_text",
				"elements": []any{map[string]any{
					"type": "rich_text_section",
					"elements": []any{map[string]any{
						"type":      "date",
						"timestamp": "1777978707",
						"format":    "{date_short_pretty}",
					}},
				}},
			}},
			want: "rich_text_section element 0 timestamp must be numeric",
		},
		{
			name: "table rich text cell elements required",
			blocks: []map[string]any{{
				"type": "table",
				"rows": []any{[]any{
					map[string]any{"type": "rich_text"},
				}},
			}},
			want: "table cell 0:0 rich_text elements are required",
		},
		{
			name:   "block count",
			blocks: tooManyBlocks,
			want:   "block count exceeds 50",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := blockkit.ValidateRawBlocks(tt.blocks)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateRawBlocks error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestValidateRawBlocksAcceptsSupportedRawBlocks(t *testing.T) {
	blocks := []map[string]any{
		{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": "hello"}},
		{"type": "context", "elements": []any{map[string]any{"type": "mrkdwn", "text": "context"}}},
		{"type": "divider"},
		{"type": "image", "image_url": "https://example.com/image.png", "alt_text": "example"},
		{"type": "file", "external_id": "F123", "source": "remote"},
		{"type": "rich_text", "elements": []any{map[string]any{"type": "rich_text_section", "elements": []any{}}}},
		{"type": "table", "rows": []any{[]any{map[string]any{"type": "rich_text", "elements": []any{}}}}},
	}
	if err := blockkit.ValidateRawBlocks(blocks); err != nil {
		t.Fatalf("ValidateRawBlocks returned error: %v", err)
	}
}

type fakeBlock struct{}

func (fakeBlock) BlockType() slackgo.MessageBlockType {
	return "fake"
}

func (fakeBlock) ID() string {
	return ""
}
