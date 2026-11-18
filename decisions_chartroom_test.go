package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// chartroomFixture writes the notes of a fixture chartroom into <ws>/chartroom/notes (Goals.md already exists there).
func chartroomFixture(t *testing.T, ws string, notes map[string]string) {
	t.Helper()
	for name, body := range notes {
		if err := os.WriteFile(filepath.Join(ws, "chartroom", "notes", name+".md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func fm(tags string) string { return "---\nid: 1\naliases: []\ntags: " + tags + "\n---\n" }

func candidateLines(out string) []string {
	var ls []string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "- ") || strings.HasPrefix(l, "# ") {
			ls = append(ls, l)
		}
	}
	return ls
}

func TestFromChartroomCandidates(t *testing.T) {
	ws, dir := decWS(t, decBase)
	long := strings.Repeat("x", 200)
	chartroomFixture(t, ws, map[string]string{
		// decided, dated by "Decision of"
		"Alpha": fm(`["type/decision", "status/decided", "category/process"]`) + "Decision of 2026-11-18 (owner): Alpha | mit Pipe.\n\nMehr.\n",
		// decided, YAML list tags, no date anywhere: the date must not be invented
		"Beta": "---\nid: 2\ntags:\n  - type/decision\n  - status/decided\n---\nOhne Datum.\n",
		// other type
		"Gamma": fm(`["type/howto", "status/decided"]`) + "Decision of 2026-11-18: andere Art.\n",
		// already a source of the register ([[Goals]] in decBase)
		"Goals": fm(`["type/decision", "status/decided"]`) + "Decision of 2026-11-03: schon im Register.\n",
		// decision, but not decided
		"Delta": fm(`["type/decision", "status/proposed"]`) + "Decision of 2026-11-18: noch offen.\n",
		// date only from "Update", heading skipped, long line cut at 160 runes
		"Epsilon": fm(`[type/decision, status/decided]`) + "# Überschrift\n\n" + long + "\nUpdate 2026-11-10: spaeter.\n",
		// same day as Alpha: next free ID; control and bidi characters masked
		"Zeta": fm(`["type/decision", "status/decided"]`) + "Decision of 2026-11-18: Er\x1b[31mste\u202e.\n",
	})
	before, _ := os.ReadFile(filepath.Join(dir, decisionsFile))
	rc, out, errOut := runDayOut(t, t.TempDir(), nil, "decisions", "from-chartroom", "--dir", dir)
	if rc != 0 {
		t.Fatalf("rc %d, err %q", rc, errOut)
	}
	got := candidateLines(out)
	want := []string{
		"- D-20261110-01 | 2026-11-10 | coordinator | proposed | workspace | - | " + strings.Repeat("x", 160) + "… | [[Epsilon]]",
		"- D-20261118-01 | 2026-11-18 | coordinator | proposed | workspace | - | Decision of 2026-11-18 (owner): Alpha | mit Pipe. | [[Alpha]]",
		`- D-20261118-02 | 2026-11-18 | coordinator | proposed | workspace | - | Decision of 2026-11-18: Er\x1b[31mste\u202e. | [[Zeta]]`,
		"# Datum fehlt: [[Beta]]",
		"- D-????????-?? | ? | coordinator | proposed | workspace | - | Ohne Datum. | [[Beta]]",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("candidates:\n%s\nwant:\n%s\n(full output %q)", strings.Join(got, "\n"), strings.Join(want, "\n"), out)
	}
	if strings.ContainsAny(out, "\x1b\u202e") {
		t.Errorf("raw control characters in the output: %q", out)
	}
	after, _ := os.ReadFile(filepath.Join(dir, decisionsFile))
	if string(before) != string(after) {
		t.Error("from-chartroom changed DECISIONS.md")
	}
}

func TestFromChartroomIDsStartAfterRegisterRows(t *testing.T) {
	ws, dir := decWS(t, decBase)
	chartroomFixture(t, ws, map[string]string{
		"Neu": fm(`["type/decision", "status/decided"]`) + "Decision of 2026-11-04 (owner): am Tag mit Zeilen.\n",
	})
	_, out, _ := runDayOut(t, t.TempDir(), nil, "decisions", "from-chartroom", "--dir", dir)
	if got := candidateLines(out); len(got) != 1 || !strings.HasPrefix(got[0], "- D-20261104-03 | 2026-11-04 | ") {
		t.Fatalf("next free ID of the day: %q", got)
	}
}

func TestFromChartroomNoCandidatesAndOption(t *testing.T) {
	ws, dir := decWS(t, decBase)
	_, out, _ := runDayOut(t, t.TempDir(), nil, "decisions", "from-chartroom", "--dir", dir)
	if !strings.Contains(out, "keine Kandidaten") {
		t.Errorf("only Goals exists and is in the register: %q", out)
	}
	other := filepath.Join(ws, "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "X.md"), []byte(fm(`["type/decision", "status/decided"]`)+"Decision of 2026-11-18: X.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rc, out, _ := runDayOut(t, t.TempDir(), nil, "decisions", "from-chartroom", "--dir", dir, "--chartroom", other)
	if got := candidateLines(out); rc != 0 || len(got) != 1 || !strings.HasSuffix(got[0], "| [[X]]") {
		t.Errorf("--chartroom: rc %d, out %q", rc, out)
	}
	if rc, _, errOut := runDayOut(t, t.TempDir(), nil, "decisions", "from-chartroom", "--dir", dir, "--chartroom", filepath.Join(ws, "nope")); rc != 2 || errOut == "" {
		t.Errorf("missing chartroom folder: rc %d, err %q", rc, errOut)
	}
}

// The proposed lines are valid register rows: appended to the register they pass the check.
func TestFromChartroomLinesPassTheCheck(t *testing.T) {
	ws, dir := decWS(t, decBase)
	chartroomFixture(t, ws, map[string]string{
		"Alpha": fm(`["type/decision", "status/decided"]`) + "Decision of 2026-11-18 (owner): Alpha | mit Pipe.\n",
		"Zeta":  fm(`["type/decision", "status/decided"]`) + "Decision of 2026-11-18: Zweite.\n",
	})
	_, out, _ := runDayOut(t, t.TempDir(), nil, "decisions", "from-chartroom", "--dir", dir)
	cands := candidateLines(out)
	if len(cands) != 2 {
		t.Fatalf("want 2 candidates, got %q", cands)
	}
	text := decBase + strings.Join(cands, "\n") + "\n"
	_, f := checkDecisionsText(text, ws, filepath.Join(ws, "chartroom", "notes"), "2026-11-18")
	if len(f) != 0 {
		t.Fatalf("findings: %v", f)
	}
}
