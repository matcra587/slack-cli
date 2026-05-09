package main

// PlainRenderer is implemented by each command data type to produce plain-mode
// output. The type switch in WritePlainResult dispatches through this interface.
// Phase 09 will move this interface and CommandContext to a shared output package.
type PlainRenderer interface {
	WritePlain(c *CommandContext, command string, pagination *Pagination) error
}
