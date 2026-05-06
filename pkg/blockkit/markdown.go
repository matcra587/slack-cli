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
		case ast.KindParagraph:
			content := strings.TrimSpace(sourceFromNode(node, source))
			if content != "" {
				blocks = append(blocks, slackgo.NewSectionBlock(MarkdownText(content), nil, nil))
			}
		case ast.KindHeading:
			content := strings.TrimSpace(textFromNode(node, source))
			if content != "" {
				blocks = append(blocks, slackgo.NewSectionBlock(MarkdownText(content), nil, nil))
			}
		case extast.KindTable:
			table := tableFromNode(node, source)
			if len(table.Rows) > 0 {
				blocks = append(blocks, table)
			}
		default:
			content := strings.TrimSpace(sourceFromNode(node, source))
			if content == "" {
				content = strings.TrimSpace(textFromNode(node, source))
			}
			if content != "" {
				blocks = append(blocks, slackgo.NewSectionBlock(MarkdownText(content), nil, nil))
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

func sourceFromNode(node ast.Node, source []byte) string {
	if node == nil {
		return ""
	}
	start := minSegmentStart(node)
	if start < 0 || start >= len(source) {
		return ""
	}

	stop := maxSegmentStop(node)
	if fenced, ok := node.(*ast.FencedCodeBlock); ok {
		if fenced.Info != nil && fenced.Info.Segment.Start >= 0 {
			start = lineStart(fenced.Info.Segment.Start, source)
		} else {
			start = previousLineStart(lineStart(start, source), source)
		}
		stop = fencedCodeStop(stop, source)
	} else {
		start = lineStart(start, source)
		stop = extendLineStop(stop, source)
	}
	if stop > len(source) {
		stop = len(source)
	}
	if stop <= start {
		return ""
	}
	return string(source[start:stop])
}

func lineStart(pos int, source []byte) int {
	if pos > len(source) {
		pos = len(source)
	}
	for pos > 0 && source[pos-1] != '\n' {
		pos--
	}
	return pos
}

func previousLineStart(pos int, source []byte) int {
	if pos <= 0 {
		return 0
	}
	pos--
	for pos > 0 && source[pos-1] != '\n' {
		pos--
	}
	return pos
}

func fencedCodeStop(stop int, source []byte) int {
	stop = extendLineStop(stop, source)
	if stop >= len(source) {
		return stop
	}
	lineEnd := extendLineStop(stop, source)
	line := strings.TrimSpace(string(source[stop:lineEnd]))
	if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
		return lineEnd
	}
	return stop
}

func minSegmentStart(node ast.Node) int {
	min := -1
	_ = ast.Walk(node, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || n.Type() != ast.TypeBlock || n.Lines() == nil {
			return ast.WalkContinue, nil
		}
		for i := range n.Lines().Len() {
			segment := n.Lines().At(i)
			if segment.Start >= 0 && (min == -1 || segment.Start < min) {
				min = segment.Start
			}
		}
		return ast.WalkContinue, nil
	})
	return min
}

func maxSegmentStop(node ast.Node) int {
	max := -1
	_ = ast.Walk(node, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || n.Type() != ast.TypeBlock || n.Lines() == nil {
			return ast.WalkContinue, nil
		}
		for i := range n.Lines().Len() {
			segment := n.Lines().At(i)
			if segment.Stop > max {
				max = segment.Stop
			}
		}
		return ast.WalkContinue, nil
	})
	return max
}

func extendLineStop(stop int, source []byte) int {
	for stop < len(source) && source[stop] != '\n' {
		stop++
	}
	if stop < len(source) && source[stop] == '\n' {
		stop++
	}
	return stop
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
			if typed.SoftLineBreak() || typed.HardLineBreak() {
				b.WriteByte('\n')
			}
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
