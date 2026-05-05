package blockkit

import (
	"errors"
	"fmt"
)

const (
	maxBlocks       = 50
	maxBlockIDLen   = 255
	maxSectionText  = 3000
	maxSectionItems = 10
	maxFieldText    = 2000
	maxContextItems = 10
	maxTableRows    = 100
	maxTableColumns = 20
)

func ValidateBlocks(blocks []Block) error {
	if len(blocks) > maxBlocks {
		return fmt.Errorf("block count exceeds %d", maxBlocks)
	}

	tableCount := 0
	for i, block := range blocks {
		switch b := block.(type) {
		case *SectionBlock:
			if err := validateBlockID(i, b.BlockID); err != nil {
				return err
			}
			if b.Text != nil && len(b.Text.Text) > maxSectionText {
				return fmt.Errorf("block %d section text exceeds %d characters", i, maxSectionText)
			}
		case SectionBlock:
			if err := validateBlockID(i, b.BlockID); err != nil {
				return err
			}
			if b.Text != nil && len(b.Text.Text) > maxSectionText {
				return fmt.Errorf("block %d section text exceeds %d characters", i, maxSectionText)
			}
		case *ContextBlock:
			if err := validateBlockID(i, b.BlockID); err != nil {
				return err
			}
			if len(b.ContextElements.Elements) > maxContextItems {
				return fmt.Errorf("block %d context elements exceed %d", i, maxContextItems)
			}
		case ContextBlock:
			if err := validateBlockID(i, b.BlockID); err != nil {
				return err
			}
			if len(b.ContextElements.Elements) > maxContextItems {
				return fmt.Errorf("block %d context elements exceed %d", i, maxContextItems)
			}
		case *DividerBlock:
			if err := validateBlockID(i, b.BlockID); err != nil {
				return err
			}
		case DividerBlock:
			if err := validateBlockID(i, b.BlockID); err != nil {
				return err
			}
		case *ImageBlock:
			if err := validateBlockID(i, b.BlockID); err != nil {
				return err
			}
			if b.ImageURL == "" {
				return fmt.Errorf("block %d image_url is required", i)
			}
			if b.AltText == "" {
				return fmt.Errorf("block %d alt_text is required", i)
			}
		case ImageBlock:
			if err := validateBlockID(i, b.BlockID); err != nil {
				return err
			}
			if b.ImageURL == "" {
				return fmt.Errorf("block %d image_url is required", i)
			}
			if b.AltText == "" {
				return fmt.Errorf("block %d alt_text is required", i)
			}
		case *FileBlock:
			if err := validateBlockID(i, b.BlockID); err != nil {
				return err
			}
			if b.ExternalID == "" || b.Source == "" {
				return fmt.Errorf("block %d file external_id and source are required", i)
			}
		case FileBlock:
			if err := validateBlockID(i, b.BlockID); err != nil {
				return err
			}
			if b.ExternalID == "" || b.Source == "" {
				return fmt.Errorf("block %d file external_id and source are required", i)
			}
		case *TableBlock:
			tableCount++
			if tableCount > 1 {
				return fmt.Errorf("messages may contain only one table block")
			}
			if err := validateTable(i, b); err != nil {
				return err
			}
		case TableBlock:
			tableCount++
			if tableCount > 1 {
				return fmt.Errorf("messages may contain only one table block")
			}
			if err := validateTable(i, &b); err != nil {
				return err
			}
		default:
			return fmt.Errorf("block %d has unsupported type %T", i, block)
		}
	}
	return nil
}

func ValidateRawBlocks(blocks []map[string]any) error {
	if len(blocks) > maxBlocks {
		return fmt.Errorf("block count exceeds %d", maxBlocks)
	}

	tableCount := 0
	for i, block := range blocks {
		blockType, _ := block["type"].(string)
		if blockType == "" {
			return fmt.Errorf("block %d type is required", i)
		}
		if err := validateRawBlockID(i, block); err != nil {
			return err
		}
		switch blockType {
		case "section":
			if err := validateRawSectionBlock(i, block); err != nil {
				return err
			}
		case "context":
			if err := validateRawContextBlock(i, block); err != nil {
				return err
			}
		case "divider":
		case "image":
			if err := validateRawImageBlock(i, block); err != nil {
				return err
			}
		case "file":
			if err := validateRawFileBlock(i, block); err != nil {
				return err
			}
		case "rich_text":
			if err := validateRawRichTextObject(i, "rich_text", block); err != nil {
				return err
			}
		case "table":
			tableCount++
			if tableCount > 1 {
				return errors.New("messages may contain only one table block")
			}
			if err := validateRawTableBlock(i, block); err != nil {
				return err
			}
		default:
			return fmt.Errorf("block %d unsupported block type %q", i, blockType)
		}
	}
	return nil
}

func validateRawBlockID(index int, block map[string]any) error {
	id, _ := block["block_id"].(string)
	if len(id) > maxBlockIDLen {
		return fmt.Errorf("block %d block_id exceeds %d characters", index, maxBlockIDLen)
	}
	return nil
}

func validateRawSectionBlock(index int, block map[string]any) error {
	text, hasText := block["text"].(map[string]any)
	fields, hasFields := block["fields"].([]any)
	if !hasText && !hasFields {
		return fmt.Errorf("block %d section text or fields are required", index)
	}
	if hasText {
		if err := validateRawTextObject(index, "section text", text, maxSectionText); err != nil {
			return err
		}
	}
	if hasFields {
		if len(fields) == 0 {
			return fmt.Errorf("block %d section fields are empty", index)
		}
		if len(fields) > maxSectionItems {
			return fmt.Errorf("block %d section fields exceed %d", index, maxSectionItems)
		}
		for fieldIndex, field := range fields {
			fieldObject, ok := field.(map[string]any)
			if !ok {
				return fmt.Errorf("block %d section field %d must be a text object", index, fieldIndex)
			}
			if err := validateRawTextObject(index, fmt.Sprintf("section field %d", fieldIndex), fieldObject, maxFieldText); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateRawContextBlock(index int, block map[string]any) error {
	elements, ok := block["elements"].([]any)
	if !ok || len(elements) == 0 {
		return fmt.Errorf("block %d context elements are required", index)
	}
	if len(elements) > maxContextItems {
		return fmt.Errorf("block %d context elements exceed %d", index, maxContextItems)
	}
	for elementIndex, element := range elements {
		elementObject, ok := element.(map[string]any)
		if !ok {
			return fmt.Errorf("block %d context element %d must be an object", index, elementIndex)
		}
		elementType, _ := elementObject["type"].(string)
		switch elementType {
		case TextTypeMarkdown, TextTypePlain:
			if err := validateRawTextObject(index, fmt.Sprintf("context element %d", elementIndex), elementObject, maxSectionText); err != nil {
				return err
			}
		case "image":
			if _, ok := elementObject["image_url"].(string); !ok || elementObject["image_url"] == "" {
				return fmt.Errorf("block %d context image element %d image_url is required", index, elementIndex)
			}
			if _, ok := elementObject["alt_text"].(string); !ok || elementObject["alt_text"] == "" {
				return fmt.Errorf("block %d context image element %d alt_text is required", index, elementIndex)
			}
		default:
			return fmt.Errorf("block %d context element %d unsupported type %q", index, elementIndex, elementType)
		}
	}
	return nil
}

func validateRawImageBlock(index int, block map[string]any) error {
	if value, _ := block["image_url"].(string); value == "" {
		return fmt.Errorf("block %d image_url is required", index)
	}
	if value, _ := block["alt_text"].(string); value == "" {
		return fmt.Errorf("block %d alt_text is required", index)
	}
	return nil
}

func validateRawFileBlock(index int, block map[string]any) error {
	externalID, _ := block["external_id"].(string)
	source, _ := block["source"].(string)
	if externalID == "" || source == "" {
		return fmt.Errorf("block %d file external_id and source are required", index)
	}
	return nil
}

func validateRawTableBlock(index int, block map[string]any) error {
	rows, ok := block["rows"].([]any)
	if !ok || len(rows) == 0 {
		return fmt.Errorf("block %d table rows are required", index)
	}
	if len(rows) > maxTableRows {
		return fmt.Errorf("block %d table rows exceed %d", index, maxTableRows)
	}
	for rowIndex, row := range rows {
		cells, ok := row.([]any)
		if !ok || len(cells) == 0 {
			return fmt.Errorf("block %d table row %d is empty", index, rowIndex)
		}
		if len(cells) > maxTableColumns {
			return fmt.Errorf("block %d table row %d exceeds %d columns", index, rowIndex, maxTableColumns)
		}
		for cellIndex, cell := range cells {
			cellObject, ok := cell.(map[string]any)
			if !ok {
				return fmt.Errorf("block %d table cell %d:%d must be an object", index, rowIndex, cellIndex)
			}
			if cellType, _ := cellObject["type"].(string); cellType != "rich_text" {
				return fmt.Errorf("block %d table cell %d:%d must be rich_text", index, rowIndex, cellIndex)
			}
			if err := validateRawRichTextObject(index, fmt.Sprintf("table cell %d:%d rich_text", rowIndex, cellIndex), cellObject); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateRawRichTextObject(index int, label string, block map[string]any) error {
	elements, ok := block["elements"].([]any)
	if !ok {
		return fmt.Errorf("block %d %s elements are required", index, label)
	}
	for elementIndex, element := range elements {
		elementObject, ok := element.(map[string]any)
		if !ok {
			return fmt.Errorf("block %d %s element %d must be an object", index, label, elementIndex)
		}
		elementType, _ := elementObject["type"].(string)
		if elementType == "" {
			return fmt.Errorf("block %d %s element %d type is required", index, label, elementIndex)
		}
		switch elementType {
		case "rich_text_section", "rich_text_list", "rich_text_quote", "rich_text_preformatted":
			if _, ok := elementObject["elements"].([]any); !ok {
				return fmt.Errorf("block %d %s element %d elements are required", index, label, elementIndex)
			}
		default:
			return fmt.Errorf("block %d %s element %d unsupported type %q", index, label, elementIndex, elementType)
		}
	}
	return nil
}

func validateRawTextObject(index int, label string, text map[string]any, maxLength int) error {
	textType, _ := text["type"].(string)
	if textType != TextTypeMarkdown && textType != TextTypePlain {
		return fmt.Errorf("block %d %s type must be mrkdwn or plain_text", index, label)
	}
	value, _ := text["text"].(string)
	if value == "" {
		return fmt.Errorf("block %d %s text is required", index, label)
	}
	if len(value) > maxLength {
		return fmt.Errorf("block %d %s text exceeds %d characters", index, label, maxLength)
	}
	return nil
}

func validateBlockID(index int, id string) error {
	if len(id) > maxBlockIDLen {
		return fmt.Errorf("block %d block_id exceeds %d characters", index, maxBlockIDLen)
	}
	return nil
}

func validateTable(index int, block *TableBlock) error {
	if err := validateBlockID(index, block.BlockID); err != nil {
		return err
	}
	if len(block.Rows) == 0 {
		return fmt.Errorf("block %d table rows are required", index)
	}
	if len(block.Rows) > maxTableRows {
		return fmt.Errorf("block %d table rows exceed %d", index, maxTableRows)
	}
	for rowIndex, row := range block.Rows {
		if len(row) == 0 {
			return fmt.Errorf("block %d table row %d is empty", index, rowIndex)
		}
		if len(row) > maxTableColumns {
			return fmt.Errorf("block %d table row %d exceeds %d columns", index, rowIndex, maxTableColumns)
		}
		for colIndex, cell := range row {
			if cell == nil || cell.Type != "rich_text" {
				return fmt.Errorf("block %d table cell %d:%d must be rich_text", index, rowIndex, colIndex)
			}
		}
	}
	return nil
}
