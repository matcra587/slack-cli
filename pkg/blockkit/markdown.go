package blockkit

import (
	"strings"

	slackgo "github.com/slack-go/slack"
	goldmark "github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

func FromMarkdown(markdown string) ([]Block, error) {
	source := []byte(markdown)
	parser := goldmark.New(goldmark.WithExtensions(extension.Table)).Parser()
	doc := parser.Parse(text.NewReader(source))

	var blocks []Block
	for node := doc.FirstChild(); node != nil; node = node.NextSibling() {
		switch node.Kind() {
		case ast.KindParagraph, ast.KindHeading:
			content := strings.TrimSpace(textFromNode(node, source))
			if content != "" {
				blocks = append(blocks, slackgo.NewSectionBlock(MarkdownText(content), nil, nil))
			}
		case extast.KindTable:
			table := tableFromNode(node, source)
			if len(table.Rows) > 0 {
				blocks = append(blocks, table)
			}
		}
	}

	if err := ValidateBlocks(blocks); err != nil {
		return nil, err
	}
	return blocks, nil
}

func tableFromNode(table ast.Node, source []byte) TableBlock {
	block := slackgo.NewTableBlock("")
	for node := table.FirstChild(); node != nil; node = node.NextSibling() {
		switch node.Kind() {
		case extast.KindTableHeader:
			if parsed := tableRowFromNode(node, source); len(parsed) > 0 {
				block.AddRow(parsed...)
			}
		case extast.KindTableRow:
			if parsed := tableRowFromNode(node, source); len(parsed) > 0 {
				block.AddRow(parsed...)
			}
		}
	}
	return *block
}

func tableRowFromNode(row ast.Node, source []byte) []*slackgo.RichTextBlock {
	var cells []*slackgo.RichTextBlock
	for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
		if cell.Kind() != extast.KindTableCell {
			continue
		}
		text := strings.TrimSpace(textFromNode(cell, source))
		cells = append(cells, RichTextCell(text))
	}
	return cells
}

func textFromNode(node ast.Node, source []byte) string {
	var b strings.Builder
	_ = ast.Walk(node, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch typed := n.(type) {
		case *ast.Text:
			b.Write(typed.Segment.Value(source))
		case *ast.CodeSpan:
			for segment := typed.FirstChild(); segment != nil; segment = segment.NextSibling() {
				if text, ok := segment.(*ast.Text); ok {
					b.Write(text.Segment.Value(source))
				}
			}
		}
		return ast.WalkContinue, nil
	})
	return b.String()
}
