package blockkit

import "fmt"

const (
	maxBlocks       = 50
	maxBlockIDLen   = 255
	maxSectionText  = 3000
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
