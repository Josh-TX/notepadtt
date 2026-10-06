package backend

import (
	"slices"
	"testing"
)

func TestExtractRange(t *testing.T) {
	lines := []string{"hello", "world", "foo"}

	t.Run("single line partial", func(t *testing.T) {
		got, ok := extractRange(lines, Pos{Line: 0, Ch: 1}, Pos{Line: 0, Ch: 4})
		if !ok || !slices.Equal(got, []string{"ell"}) {
			t.Fatalf("got %v, %v", got, ok)
		}
	})

	t.Run("multi line", func(t *testing.T) {
		got, ok := extractRange(lines, Pos{Line: 0, Ch: 3}, Pos{Line: 2, Ch: 2})
		if !ok || !slices.Equal(got, []string{"lo", "world", "fo"}) {
			t.Fatalf("got %v, %v", got, ok)
		}
	})

	t.Run("out of range line fails", func(t *testing.T) {
		if _, ok := extractRange(lines, Pos{Line: 0, Ch: 0}, Pos{Line: 5, Ch: 0}); ok {
			t.Fatalf("expected out-of-range failure")
		}
	})

	t.Run("out of range ch fails", func(t *testing.T) {
		if _, ok := extractRange(lines, Pos{Line: 0, Ch: 0}, Pos{Line: 0, Ch: 99}); ok {
			t.Fatalf("expected out-of-range failure")
		}
	})
}

func TestReplaceRange(t *testing.T) {
	t.Run("insert within line", func(t *testing.T) {
		lines := []string{"hello", "world"}
		got, ok := replaceRange(lines, Pos{Line: 0, Ch: 5}, Pos{Line: 0, Ch: 5}, []string{"!"})
		if !ok || !slices.Equal(got, []string{"hello!", "world"}) {
			t.Fatalf("got %v, %v", got, ok)
		}
	})

	t.Run("multi-line replace collapses to fewer lines", func(t *testing.T) {
		lines := []string{"line1", "line2", "line3"}
		got, ok := replaceRange(lines, Pos{Line: 0, Ch: 2}, Pos{Line: 2, Ch: 2}, []string{"X"})
		if !ok || !slices.Equal(got, []string{"liXne3"}) {
			t.Fatalf("got %v, %v", got, ok)
		}
	})

	t.Run("single line replace expands to more lines", func(t *testing.T) {
		lines := []string{"hello world"}
		got, ok := replaceRange(lines, Pos{Line: 0, Ch: 5}, Pos{Line: 0, Ch: 6}, []string{"", ""})
		if !ok || !slices.Equal(got, []string{"hello", "world"}) {
			t.Fatalf("got %v, %v", got, ok)
		}
	})
}

// A line inserted at the very start should shift every later old line number by one
// in the mapping, while still being reported as unmodified (equal).
func TestBuildLineMapping(t *testing.T) {
	old := "line1\nline2\nline3"
	new := "inserted\nline1\nline2\nline3"
	oldToNew, equalLine := buildLineMapping(old, new)

	for i := 0; i < 3; i++ {
		if !equalLine[i] {
			t.Fatalf("expected old line %d to be unmodified", i)
		}
		if oldToNew[i] != i+1 {
			t.Fatalf("oldToNew[%d] = %d, want %d", i, oldToNew[i], i+1)
		}
	}
}
