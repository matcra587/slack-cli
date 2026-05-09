package blockkit

import (
	"strings"
	"sync"
)

var defaultStripReplacer = sync.OnceValue(func() *strings.Replacer {
	return strings.NewReplacer("*", "", "_", "", "`", "")
})

func RenderMarkdown(blocks []Block) string {
	var b strings.Builder
	for _, block := range blocks {
		switch typed := block.(type) {
		case *SectionBlock:
			if typed.Text != nil {
				writeLine(&b, typed.Text.Text)
			}
		case SectionBlock:
			if typed.Text != nil {
				writeLine(&b, typed.Text.Text)
			}
		case *ContextBlock:
			for _, element := range typed.ContextElements.Elements {
				if text, ok := element.(*TextObject); ok {
					writeLine(&b, text.Text)
				}
			}
		case ContextBlock:
			for _, element := range typed.ContextElements.Elements {
				if text, ok := element.(*TextObject); ok {
					writeLine(&b, text.Text)
				}
			}
		case *TableBlock:
			renderTable(&b, typed)
		case TableBlock:
			renderTable(&b, &typed)
		}
	}
	return b.String()
}

func RenderPlain(blocks []Block) string {
	return stripMarkdown(RenderMarkdown(blocks))
}

func renderTable(b *strings.Builder, table *TableBlock) {
	for _, row := range table.Rows {
		cells := make([]string, 0, len(row))
		for _, cell := range row {
			cells = append(cells, richTextPlain(cell))
		}
		writeLine(b, strings.Join(cells, "\t"))
	}
}

func richTextPlain(block *RichTextBlock) string {
	if block == nil {
		return ""
	}
	var b strings.Builder
	for _, element := range block.Elements {
		section, ok := element.(*RichTextSection)
		if !ok {
			continue
		}
		for _, sectionElement := range section.Elements {
			if text, ok := sectionElement.(*RichTextSectionTextElement); ok {
				b.WriteString(text.Text)
			}
		}
	}
	return b.String()
}

func writeLine(b *strings.Builder, line string) {
	if line == "" {
		return
	}
	b.WriteString(line)
	b.WriteByte('\n')
}

func stripMarkdown(s string) string {
	return defaultStripReplacer().Replace(s)
}
