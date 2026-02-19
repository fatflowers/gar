package tool

import "testing"

func TestNewCodingTools(t *testing.T) {
	t.Parallel()

	got := NewCodingTools()
	if len(got) != 4 {
		t.Fatalf("len(NewCodingTools()) = %d, want 4", len(got))
	}
	want := []string{"read", "bash", "edit", "write"}
	for i, tool := range got {
		if tool.Name() != want[i] {
			t.Fatalf("tool[%d].Name() = %q, want %q", i, tool.Name(), want[i])
		}
	}
}

func TestNewReadOnlyTools(t *testing.T) {
	t.Parallel()

	got := NewReadOnlyTools()
	if len(got) != 4 {
		t.Fatalf("len(NewReadOnlyTools()) = %d, want 4", len(got))
	}
	want := []string{"read", "grep", "find", "ls"}
	for i, tool := range got {
		if tool.Name() != want[i] {
			t.Fatalf("tool[%d].Name() = %q, want %q", i, tool.Name(), want[i])
		}
	}
}

func TestNewAllTools(t *testing.T) {
	t.Parallel()

	got := NewAllTools()
	if len(got) != 7 {
		t.Fatalf("len(NewAllTools()) = %d, want 7", len(got))
	}
}
