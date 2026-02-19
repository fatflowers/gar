package tool

import agenttool "gar/internal/agent/tool"

// NewCodingTools returns the default coding tool set.
func NewCodingTools() []agenttool.Tool {
	return []agenttool.Tool{
		agenttool.NewReadTool(),
		agenttool.NewBashTool(),
		agenttool.NewEditTool(),
		agenttool.NewWriteTool(),
	}
}

// NewReadOnlyTools returns the read-only exploration tool set.
func NewReadOnlyTools() []agenttool.Tool {
	return []agenttool.Tool{
		agenttool.NewReadTool(),
		agenttool.NewGrepTool(),
		agenttool.NewFindTool(),
		agenttool.NewLsTool(),
	}
}

// NewAllTools returns all available built-in tools.
func NewAllTools() []agenttool.Tool {
	return []agenttool.Tool{
		agenttool.NewReadTool(),
		agenttool.NewBashTool(),
		agenttool.NewEditTool(),
		agenttool.NewWriteTool(),
		agenttool.NewGrepTool(),
		agenttool.NewFindTool(),
		agenttool.NewLsTool(),
	}
}
