package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// supersede runs add with --supersedes id and returns rc, stdout, stderr.
func supersede(t *testing.T, dir, id string, extra map[string]string) (int, string, string) {
	t.Helper()
	m := map[string]string{"--supersedes": id}
	for k, v := range extra {
		m[k] = v
	}
	return runDayOut(t, t.TempDir(), nil, addArgs(dir, m)...)
}

func TestSupersedeChangesOnlyTheReplacedRowAndAppends(t *testing.T) {
	ws, dir := decWS(t, decBase)
	reg := filepath.Join(dir, decisionsFile)
	rc, out, errOut := supersede(t, dir, "D-20261104-01", map[string]string{"--text": "Dritte neu."})
	if rc != 0 {
		t.Fatalf("rc %d, out %q, err %q", rc, out, errOut)
	}
	old := strings.Split(decBase, "\n")
	want := append([]string{}, old...)
	want[6] = "- D-20261104-01 | 2026-11-04 | owner | superseded | day | D-20261103-02 | Dritte | mit | Pipe; ersetzt durch D-20261118-01. | dayboard/LOGBOOK.md"
	want[len(want)-1] = "- D-20261118-01 | 2026-11-18 | owner | decided | day | D-20261104-01 | Dritte neu. | chat 2026-11-18"
	want = append(want, "")
	if got := readFile(t, reg); got != strings.Join(want, "\n") {
		t.Fatalf("file:\n%s\nwant:\n%s", got, strings.Join(want, "\n"))
	}
	if _, f := checkDecisionsText(readFile(t, reg), ws, filepath.Join(ws, "chartroom", "notes"), "2026-11-18"); len(f) != 0 {
		t.Errorf("findings after supersede: %v", f)
	}
	if !strings.Contains(out, "D-20261104-01") || !strings.Contains(out, "superseded") {
		t.Errorf("the changed old row is not reported: %q", out)
	}
}

func TestSupersedeTextWithoutTrailingPeriod(t *testing.T) {
	_, dir := decWS(t, strings.Replace(decBase, "Dritte | mit | Pipe.", "Dritte ohne Punkt", 1))
	if rc, out, _ := supersede(t, dir, "D-20261104-01", nil); rc != 0 {
		t.Fatalf("rc %d, out %q", rc, out)
	}
	if got := readFile(t, filepath.Join(dir, decisionsFile)); !strings.Contains(got, "| Dritte ohne Punkt; ersetzt durch D-20261118-01. | dayboard/LOGBOOK.md\n") {
		t.Errorf("file: %q", got)
	}
}

func TestSupersedeDryRun(t *testing.T) {
	_, dir := decWS(t, decBase)
	rc, out, _ := supersede(t, dir, "D-20261104-01", map[string]string{"--dry-run": ""})
	if rc != 0 || !strings.Contains(out, "dry-run") || !strings.Contains(out, "ersetzt durch D-20261118-01.") || !strings.Contains(out, "| D-20261104-01 | Neu. |") {
		t.Fatalf("rc %d, out %q", rc, out)
	}
	if readFile(t, filepath.Join(dir, decisionsFile)) != decBase {
		t.Error("dry-run changed the file")
	}
}

// No write at all when the new file would be invalid: the old row stays as it was, too.
func TestSupersedeRefusalsLeaveTheFileUnchanged(t *testing.T) {
	for _, c := range []struct {
		name, id string
		extra    map[string]string
		rc       int
		want     string
	}{
		{"unknown ID", "D-20250101-01", nil, 2, "D-20250101-01 steht nicht im Register"},
		{"malformed ID", "D-1", nil, 2, "--supersedes: ungültige ID"},
		{"already superseded", "D-20261103-02", nil, 1, "D-20261103-02 ist bereits superseded"},
		{"proposal replaces", "D-20261104-01", map[string]string{"--by": "coordinator", "--status": "proposed"}, 1, "ein Vorschlag (proposed) ersetzt keine Zeile"},
		{"replaced row is a proposal", "D-20261104-02", nil, 1, "superseded braucht Entscheider owner, nicht coordinator"},
		{"new row invalid", "D-20261104-01", map[string]string{"--scope": "Work Space"}, 1, "ungültige Geltung Work Space"},
		{"new row source invalid", "D-20261104-01", map[string]string{"--source": "[[Nope]]"}, 1, "Chartroom-Notiz `Nope` nicht gefunden"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, dir := decWS(t, decBase)
			rc, out, errOut := supersede(t, dir, c.id, c.extra)
			if rc != c.rc || !strings.Contains(out+errOut, c.want) {
				t.Fatalf("rc %d, out %q, err %q, want rc %d and %q", rc, out, errOut, c.rc, c.want)
			}
			if readFile(t, filepath.Join(dir, decisionsFile)) != decBase {
				t.Error("the register changed")
			}
		})
	}
}

func TestSupersedeChain(t *testing.T) {
	ws, dir := decWS(t, decBase)
	if rc, out, _ := supersede(t, dir, "D-20261104-01", nil); rc != 0 {
		t.Fatalf("first: rc %d, out %q", rc, out)
	}
	if rc, out, _ := supersede(t, dir, "D-20261118-01", map[string]string{"--text": "Dritte, dritte."}); rc != 0 {
		t.Fatalf("second: rc %d, out %q", rc, out)
	}
	got := readFile(t, filepath.Join(dir, decisionsFile))
	if !strings.Contains(got, "| owner | superseded | day | D-20261104-01 | Neu; ersetzt durch D-20261118-02. | chat 2026-11-18\n") ||
		!strings.HasSuffix(got, "- D-20261118-02 | 2026-11-18 | owner | decided | day | D-20261118-01 | Dritte, dritte. | chat 2026-11-18\n") {
		t.Errorf("file: %q", got)
	}
	if _, f := checkDecisionsText(got, ws, filepath.Join(ws, "chartroom", "notes"), "2026-11-18"); len(f) != 0 {
		t.Errorf("findings: %v", f)
	}
}
