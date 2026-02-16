# gar

gar (Go Agent Runtime) 是一个使用 Go 编写的极简 TUI 编程助手。

## 当前状态

核心运行模块已实现并通过测试：
- 统一的 `internal/llm` 层（Anthropic + mock provider）
- 带工具执行、steering/follow-up 队列与取消机制的 Agent 循环
- 内置工具：`read`、`write`、`edit`、`bash`
- Session JSONL 持久化与 TUI Session 记录器
- 基于 BubbleTea 的 TUI 骨架与 Cobra CLI 入口
