package rules

import (
	"strings"
	"testing"

	"github.com/jagannivas/throttle/internal/core"
)

func TestMergeOrder(t *testing.T) {
	s := New()
	s.SetGlobal([]string{"g1"})
	s.SetTool(core.ToolClaude, []string{"t1"})
	s.SetSession("S", []string{"s1"})

	got := s.RulesFor(core.ToolClaude, "S")
	want := []string{"g1", "t1", "s1"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("merge = %v, want %v", got, want)
	}

	// A different tool/session sees only global.
	if r := s.RulesFor(core.ToolCodex, "other"); strings.Join(r, ",") != "g1" {
		t.Fatalf("isolation broken: %v", r)
	}
}

func TestOneOffDrains(t *testing.T) {
	s := New()
	s.Enqueue("S", "ship it")
	s.Enqueue("S", "carefully")
	first := s.DrainOneOff("S")
	if len(first) != 2 {
		t.Fatalf("drain = %v, want 2", first)
	}
	if second := s.DrainOneOff("S"); len(second) != 0 {
		t.Fatalf("second drain should be empty, got %v", second)
	}
}

func TestInjectTextFormat(t *testing.T) {
	out := InjectText([]string{"no force-push", "run tests first"}, []string{"focus on the parser"})
	if !strings.Contains(out, "1. no force-push") || !strings.Contains(out, "2. run tests first") {
		t.Fatalf("rules not numbered:\n%s", out)
	}
	if !strings.Contains(out, "operator] focus on the parser") {
		t.Fatalf("one-off missing:\n%s", out)
	}
}

func TestInjectTextEmpty(t *testing.T) {
	if InjectText(nil, nil) != "" {
		t.Fatal("empty rules should inject nothing")
	}
	if InjectText([]string{"  ", ""}, nil) != "" {
		t.Fatal("blank rules should inject nothing")
	}
}
