package blockkit_test

import (
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/pkg/blockkit"
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
