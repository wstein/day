package main

import (
	"strings"
	"testing"
)

func TestSections(t *testing.T) {
	lines := strings.Split(template("2026-11-04"), "\n")
	s := sections(lines)
	for _, n := range secs {
		if s[n] == nil {
			t.Fatalf("section %q missing", n)
		}
	}
	// the last section ends at the facts marker, not at end of file
	if e := s["Erledigt heute"][1]; strings.TrimSpace(lines[e]) != begin {
		t.Fatalf("Erledigt heute ends at %q", lines[e])
	}
}

func TestHeadRe(t *testing.T) {
	if m := headRe.FindStringSubmatch("# Dayboard (Stand 2026-11-04)"); m == nil || m[1] != "2026-11-04" {
		t.Fatal("valid header not matched")
	}
	if headRe.MatchString("# Dayboard (Stand 2026-11-4)") {
		t.Fatal("invalid header matched")
	}
}
