//go:build w8o2

package mermaidlint

import (
	"strings"
	"testing"
)

func lint(lines ...string) []string { return Lint(strings.Join(lines, "\n")) }

func TestW8O2_AValidSequenceDiagramHasNoProblems(t *testing.T) {
	got := lint(
		"sequenceDiagram",
		"    autonumber",
		"    participant App",
		"    participant API as Go API",
		"    actor Rider",
		"    App->>API: POST /v1/orders",
		"    API-->>App: 201 order",
		"    Note over API: outbox row written",
		"    Rider-)API: location",
	)
	if len(got) != 0 {
		t.Fatalf("Lint() = %v, want none", got)
	}
}

func TestW8O2_TheFirstLineMustBeSequenceDiagram(t *testing.T) {
	got := lint("flowchart LR", "    A --> B")
	if len(got) != 1 || got[0] != "line 1: a sequence diagram must start with sequenceDiagram" {
		t.Fatalf("Lint() = %q", got)
	}
}

func TestW8O2_AnEmptyDiagramIsAProblem(t *testing.T) {
	if got := Lint("   \n\n"); len(got) != 1 || got[0] != "line 1: the diagram is empty" {
		t.Fatalf("Lint() = %q", got)
	}
}

func TestW8O2_UndeclaredParticipantsAreReportedWithTheirLine(t *testing.T) {
	got := lint(
		"sequenceDiagram",
		"    participant App",
		"    App->>Cache: GET",
		"    Worker->>App: done",
	)
	want := []string{
		"line 3: undeclared participant Cache",
		"line 4: undeclared participant Worker",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("Lint() = %q, want %q", got, want)
	}
}

func TestW8O2_ParticipantAliasesAreDeclarationsToo(t *testing.T) {
	if got := lint("sequenceDiagram", "    participant DB as MySQL", "    DB->>DB: commit"); len(got) != 0 {
		t.Fatalf("Lint() = %v, want none", got)
	}
}

func TestW8O2_EveryArrowStyleIsRecognised(t *testing.T) {
	for _, arrow := range []string{"->>", "-->>", "->", "-->", "-x", "--x", "-)", "--)"} {
		t.Run(arrow, func(t *testing.T) {
			if got := lint("sequenceDiagram", "    participant A", "    participant B", "    A"+arrow+"B: hi"); len(got) != 0 {
				t.Fatalf("Lint() = %v, want none", got)
			}
		})
	}
}

func TestW8O2_AMessageNeedsText(t *testing.T) {
	got := lint("sequenceDiagram", "    participant A", "    participant B", "    A->>B:", "    A->>B")
	want := "line 4: missing message text|line 5: missing message text"
	if strings.Join(got, "|") != want {
		t.Fatalf("Lint() = %q, want %q", got, want)
	}
}

func TestW8O2_UnrecognisedLinesAreReported(t *testing.T) {
	got := lint("sequenceDiagram", "    participant A", "    this is not mermaid")
	if len(got) != 1 || got[0] != "line 3: unrecognised line" {
		t.Fatalf("Lint() = %q", got)
	}
}

func TestW8O2_CommentsAndBlankLinesAreIgnored(t *testing.T) {
	if got := lint("sequenceDiagram", "", "    %% a comment", "    participant A", "    A->>A: tick"); len(got) != 0 {
		t.Fatalf("Lint() = %v, want none", got)
	}
}

func TestW8O2_BlocksMustBeClosed(t *testing.T) {
	ok := lint("sequenceDiagram", "    participant A", "    loop every second", "        A->>A: tick", "    end")
	if len(ok) != 0 {
		t.Fatalf("balanced loop: Lint() = %v, want none", ok)
	}
	open := lint("sequenceDiagram", "    participant A", "    alt ok", "        A->>A: yes", "    else failed", "        A->>A: no")
	if len(open) != 1 || open[0] != "line 3: block opened here is never closed with end" {
		t.Fatalf("unclosed alt: Lint() = %q", open)
	}
	stray := lint("sequenceDiagram", "    participant A", "    end")
	if len(stray) != 1 || stray[0] != "line 3: end without an open block" {
		t.Fatalf("stray end: Lint() = %q", stray)
	}
}

func TestW8O2_ActivateNeedsADeclaredParticipant(t *testing.T) {
	got := lint("sequenceDiagram", "    participant A", "    activate A", "    deactivate B")
	if len(got) != 1 || got[0] != "line 4: undeclared participant B" {
		t.Fatalf("Lint() = %q", got)
	}
}
