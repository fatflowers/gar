package tui

import (
	"fmt"
	"strings"
	"testing"
)

func TestChatModelRenderUsesViewportAndScroll(t *testing.T) {
	t.Parallel()

	chat := NewChatModel(0)
	chat.SetViewportHeight(3)
	theme := ResolveTheme("dark")

	for i := 1; i <= 5; i++ {
		chat.Append("user", fmt.Sprintf("m%d", i))
	}

	rendered := chat.Render(80, theme)
	if strings.Contains(rendered, "m1") || strings.Contains(rendered, "m2") {
		t.Fatalf("expected initial render at bottom, got %q", rendered)
	}
	if !strings.Contains(rendered, "m3") || !strings.Contains(rendered, "m5") {
		t.Fatalf("expected bottom window to include m3..m5, got %q", rendered)
	}

	chat.ScrollUp(2)
	rendered = chat.Render(80, theme)
	if !strings.Contains(rendered, "m1") || !strings.Contains(rendered, "m3") {
		t.Fatalf("expected scrolled render to include m1..m3, got %q", rendered)
	}
	if strings.Contains(rendered, "m5") {
		t.Fatalf("expected scrolled render to exclude m5, got %q", rendered)
	}
}
