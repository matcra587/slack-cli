package output

// PlainRenderer is implemented by each command data type to produce plain-mode
// output. The type switch in WritePlainResult dispatches through this interface.
type PlainRenderer interface {
	WritePlain(c *CommandContext, command string, pagination *Pagination) error
}
