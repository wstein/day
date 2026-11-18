package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// addArgs builds the arguments of `day decisions add` for the dayboard dir; extra replaces the default value of a flag.
func addArgs(dir string, extra map[string]string) []string {
	f := map[string]string{"--by": "owner", "--status": "decided", "--scope": "day", "--text": "Neu.", "--source": "chat 2026-11-18", "--date": "2026-11-18"}
	for k, v := range extra {
		f[k] = v
	}
	a := []string{"decisions", "add", "--dir", dir}
	for _, k := range []string{"--by", "--status", "--scope", "--text", "--source", "--date"} {
		if v, ok := f[k]; ok {
			a = append(a, k, v)
		}
		delete(f, k)
	}
	for k, v := range f { // other flags (--supersedes, ...); "" = flag without value
		a = append(a, k)
		if v != "" {
			a = append(a, v)
		}
	}
	return a
}

func TestAddAppendsOneRow(t *testing.T) {
	_, dir := decWS(t, decBase)
	reg := filepath.Join(dir, decisionsFile)
	rc, out, errOut := runDayOut(t, t.TempDir(), nil, addArgs(dir, map[string]string{"--text": "Neu | mit Pipe."})...)
	row := "- D-20261118-01 | 2026-11-18 | owner | decided | day | - | Neu | mit Pipe. | chat 2026-11-18\n"
	if rc != 0 {
		t.Fatalf("rc %d, out %q, err %q", rc, out, errOut)
	}
	if got := readFile(t, reg); got != decBase+row {
		t.Fatalf("file:\n%q\nwant:\n%q", got, decBase+row)
	}
	if !strings.Contains(out, strings.TrimSuffix(row, "\n")) {
		t.Errorf("the new row is not shown: %q", out)
	}
	// second add on the same day: next free ID
	rc, _, _ = runDayOut(t, t.TempDir(), nil, addArgs(dir, nil)...)
	if got := readFile(t, reg); rc != 0 || !strings.HasSuffix(got, "- D-20261118-02 | 2026-11-18 | owner | decided | day | - | Neu. | chat 2026-11-18\n") {
		t.Fatalf("second add: rc %d, file %q", rc, got)
	}
	// no leftovers next to the register
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".tmp") || strings.HasSuffix(e.Name(), ".lock") {
			t.Errorf("leftover file %s", e.Name())
		}
	}
}

func TestAddNextFreeIDOfTheDate(t *testing.T) {
	_, dir := decWS(t, decBase)
	rc, _, errOut := runDayOut(t, t.TempDir(), nil, addArgs(dir, map[string]string{"--date": "2026-11-04", "--source": "chat 2026-11-04"})...)
	if got := readFile(t, filepath.Join(dir, decisionsFile)); rc != 0 || !strings.Contains(got, "- D-20261104-03 | 2026-11-04 | owner | decided | day | - | Neu. | chat 2026-11-04\n") {
		t.Fatalf("rc %d, err %q, file %q", rc, errOut, got)
	}
}

func TestAddDefaultDateIsToday(t *testing.T) {
	_, dir := decWS(t, decBase)
	today := time.Now().Format("2006-01-02")
	args := addArgs(dir, map[string]string{"--source": "chat " + today})
	var noDate []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--date" {
			i++
			continue
		}
		noDate = append(noDate, args[i])
	}
	rc, _, errOut := runDayOut(t, t.TempDir(), nil, noDate...)
	if got := readFile(t, filepath.Join(dir, decisionsFile)); rc != 0 || !strings.Contains(got, "| "+today+" | owner | decided |") {
		t.Fatalf("rc %d, err %q, file %q", rc, errOut, got)
	}
}

func TestAddDryRunWritesNothing(t *testing.T) {
	_, dir := decWS(t, decBase)
	rc, out, _ := runDayOut(t, t.TempDir(), nil, addArgs(dir, map[string]string{"--dry-run": ""})...)
	if rc != 0 || !strings.Contains(out, "- D-20261118-01 | 2026-11-18 | owner | decided | day | - | Neu. | chat 2026-11-18") || !strings.Contains(out, "dry-run") {
		t.Fatalf("rc %d, out %q", rc, out)
	}
	if readFile(t, filepath.Join(dir, decisionsFile)) != decBase {
		t.Error("dry-run changed the file")
	}
	// a violation is also reported by a dry run
	rc, out, _ = runDayOut(t, t.TempDir(), nil, addArgs(dir, map[string]string{"--dry-run": "", "--scope": "Work Space"})...)
	if rc != 1 || !strings.Contains(out, "ungültige Geltung Work Space") {
		t.Fatalf("dry-run violation: rc %d, out %q", rc, out)
	}
}

// A violation: exit 1, the findings on stdout, the register byte-identical.
func TestAddViolationsLeaveTheFileUnchanged(t *testing.T) {
	for _, c := range []struct {
		name  string
		extra map[string]string
		want  string
	}{
		{"agent decides", map[string]string{"--by": "agent:author"}, "decided braucht Entscheider owner, nicht agent:author"},
		{"coordinator decides", map[string]string{"--by": "coordinator"}, "decided braucht Entscheider owner, nicht coordinator"},
		{"owner proposes", map[string]string{"--status": "proposed"}, "proposed braucht Entscheider coordinator oder agent:<rolle>, nicht owner"},
		{"bad scope", map[string]string{"--scope": "Work Space"}, "ungültige Geltung Work Space"},
		{"empty text", map[string]string{"--text": ""}, "leere Aussage"},
		{"text with edge space", map[string]string{"--text": " Neu."}, "Feld mit Leerzeichen am Rand"},
		{"chat date differs", map[string]string{"--source": "chat 2026-11-17"}, "chat-Datum 2026-11-17 ungleich Zeilendatum 2026-11-18"},
		{"unknown note", map[string]string{"--source": "[[Keine Notiz]]"}, "Chartroom-Notiz `Keine Notiz` nicht gefunden"},
		{"path with ..", map[string]string{"--source": "dayboard/../dayboard/LOGBOOK.md"}, "ungültige Quelle"},
		{"absolute path", map[string]string{"--source": "/etc/passwd"}, "ungültige Quelle"},
		{"missing path", map[string]string{"--source": "dayboard/NOPE.md"}, "Pfad `dayboard/NOPE.md` nicht gefunden"},
		{"date before the last row", map[string]string{"--date": "2026-11-01", "--source": "chat 2026-11-01"}, "IDs nicht aufsteigend bei D-20261101-01"},
		{"bad agent role", map[string]string{"--by": "agent:Author", "--status": "proposed"}, "proposed braucht Entscheider"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, dir := decWS(t, decBase)
			rc, out, errOut := runDayOut(t, t.TempDir(), nil, addArgs(dir, c.extra)...)
			if rc != 1 || !strings.Contains(out, c.want) {
				t.Fatalf("rc %d, out %q, err %q, want a finding %q", rc, out, errOut, c.want)
			}
			if readFile(t, filepath.Join(dir, decisionsFile)) != decBase {
				t.Error("the register changed")
			}
		})
	}
}

func TestAddAllowedDeciders(t *testing.T) {
	for _, extra := range []map[string]string{
		{"--by": "coordinator", "--status": "proposed"},
		{"--by": "agent:author", "--status": "proposed"},
		{"--by": "agent:review-2", "--status": "proposed"},
		{"--by": "owner", "--status": "decided"},
	} {
		_, dir := decWS(t, decBase)
		if rc, out, _ := runDayOut(t, t.TempDir(), nil, addArgs(dir, extra)...); rc != 0 {
			t.Errorf("%v: rc %d, out %q", extra, rc, out)
		}
	}
}

func TestAddRefusesAnInvalidRegister(t *testing.T) {
	bad := strings.Replace(decBase, "| decided | day |", "| agreed | day |", 1)
	_, dir := decWS(t, bad)
	rc, out, _ := runDayOut(t, t.TempDir(), nil, addArgs(dir, nil)...)
	if rc != 1 || !strings.Contains(out, "ungültiger Status agreed") {
		t.Fatalf("rc %d, out %q", rc, out)
	}
	if readFile(t, filepath.Join(dir, decisionsFile)) != bad {
		t.Error("the register changed")
	}
}

// Input that would break the line format is refused with exit 2 and a message; the register stays unchanged.
func TestAddRejectsUnsafeInput(t *testing.T) {
	for _, c := range []struct {
		name, flag, val, want string
	}{
		{"newline in text", "--text", "a\nb", "--text: Zeilenumbruch"},
		{"CR in text", "--text", "a\rb", "--text: Zeilenumbruch"},
		{"tab in text", "--text", "a\tb", "--text: Zeilenumbruch"},
		{"escape in text", "--text", "a\x1b[31mb", "--text: Zeilenumbruch"},
		{"bidi in text", "--text", "a\u202eb", "--text: Zeilenumbruch"},
		{"line separator in text", "--text", "a\u2028b", "--text: Zeilenumbruch"},
		{"newline in source", "--source", "chat 2026-11-18\n- D-x", "--source: Zeilenumbruch"},
		{"newline in scope", "--scope", "day\nx", "--scope: Zeilenumbruch"},
		{"newline in by", "--by", "owner\nx", "--by: Zeilenumbruch"},
		{"separator in source", "--source", "a | chat 2026-11-18", "--source: ' | ' nicht erlaubt"},
		{"separator in scope", "--scope", "a | b", "--scope: ' | ' nicht erlaubt"},
		{"separator in by", "--by", "owner | x", "--by: ' | ' nicht erlaubt"},
		{"invalid UTF-8 in text", "--text", "a\xffb", "--text: Zeilenumbruch"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, dir := decWS(t, decBase)
			rc, out, errOut := runDayOut(t, t.TempDir(), nil, addArgs(dir, map[string]string{c.flag: c.val})...)
			if rc != 2 || !strings.Contains(errOut, c.want) {
				t.Fatalf("rc %d, out %q, err %q, want exit 2 and %q", rc, out, errOut, c.want)
			}
			if readFile(t, filepath.Join(dir, decisionsFile)) != decBase {
				t.Error("the register changed")
			}
		})
	}
}

func TestAddArgumentErrors(t *testing.T) {
	_, dir := decWS(t, decBase)
	for _, miss := range []string{"--by", "--status", "--scope", "--text", "--source"} {
		args := addArgs(dir, nil)
		var cut []string
		for i := 0; i < len(args); i++ {
			if args[i] == miss {
				i++
				continue
			}
			cut = append(cut, args[i])
		}
		if rc, _, errOut := runDayOut(t, t.TempDir(), nil, cut...); rc != 2 || !strings.Contains(errOut, miss) {
			t.Errorf("without %s: rc %d, err %q", miss, rc, errOut)
		}
	}
	for _, extra := range []map[string]string{{"--status": "superseded"}, {"--status": "revoked"}, {"--status": "x"}, {"--date": "2026-13-01"}} {
		if rc, _, errOut := runDayOut(t, t.TempDir(), nil, addArgs(dir, extra)...); rc != 2 || errOut == "" {
			t.Errorf("%v: rc %d, err %q", extra, rc, errOut)
		}
	}
	if readFile(t, filepath.Join(dir, decisionsFile)) != decBase {
		t.Error("the register changed")
	}
	_, dir2 := decWS(t, "")
	rc, _, errOut := runDayOut(t, t.TempDir(), nil, addArgs(dir2, nil)...)
	if _, err := os.Stat(filepath.Join(dir2, decisionsFile)); rc != 2 || !strings.Contains(errOut, "DECISIONS.md fehlt") || err == nil {
		t.Errorf("missing register: rc %d, err %q (the file must not be created)", rc, errOut)
	}
}

func TestAddChartroomOption(t *testing.T) {
	ws, dir := decWS(t, decBase)
	other := filepath.Join(ws, "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "Eigene.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	args := addArgs(dir, map[string]string{"--source": "[[Eigene]]"})
	if rc, _, _ := runDayOut(t, t.TempDir(), nil, args...); rc != 1 {
		t.Fatalf("default chartroom has no note Eigene: rc %d", rc)
	}
	if rc, out, _ := runDayOut(t, t.TempDir(), nil, append(args, "--chartroom", other)...); rc != 0 {
		t.Fatalf("--chartroom: rc %d, out %q", rc, out)
	}
}

func TestAddTouchesOnlyTheRegister(t *testing.T) {
	_, dir := decWS(t, decBase)
	before := map[string]string{}
	for _, n := range []string{"DAYBOARD.md", "LOGBOOK.md"} {
		before[n] = readFile(t, filepath.Join(dir, n))
	}
	if rc, _, _ := runDayOut(t, t.TempDir(), nil, addArgs(dir, nil)...); rc != 0 {
		t.Fatal("add failed")
	}
	for n, b := range before {
		if readFile(t, filepath.Join(dir, n)) != b {
			t.Errorf("%s changed", n)
		}
	}
}

// Two (here: eight) concurrent adds: eight different IDs, no lost row, the register passes the check.
func TestAddConcurrentWritersLoseNoRow(t *testing.T) {
	ws, dir := decWS(t, decBase)
	const n = 8
	var wg sync.WaitGroup
	rcs := make([]int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			args := addArgs(dir, map[string]string{"--text": "Parallel " + string(rune('A'+i)) + "."})
			c := exec.Command(os.Args[0], append([]string{"-test.run=TestHelperProcess", "--"}, args...)...)
			c.Env = append(os.Environ(), "DAY_HELPER=1", "DAY_DIR=")
			rcs[i] = 0
			if err := c.Run(); err != nil {
				rcs[i] = -1
				if ee, ok := err.(*exec.ExitError); ok {
					rcs[i] = ee.ExitCode()
				}
			}
		}(i)
	}
	wg.Wait()
	for i, rc := range rcs {
		if rc != 0 {
			t.Errorf("writer %d: rc %d", i, rc)
		}
	}
	got := readFile(t, filepath.Join(dir, decisionsFile))
	for i := 1; i <= n; i++ {
		id := "D-20261118-0" + string(rune('0'+i))
		if strings.Count(got, "| "+id+" |")+strings.Count(got, "- "+id+" |") != 1 {
			t.Errorf("ID %s is missing or twice in:\n%s", id, got)
		}
	}
	for i := 0; i < n; i++ {
		if !strings.Contains(got, "Parallel "+string(rune('A'+i))+".") {
			t.Errorf("row of writer %d is lost", i)
		}
	}
	if _, f := checkDecisionsText(got, ws, filepath.Join(ws, "chartroom", "notes"), "2026-11-18"); len(f) != 0 {
		t.Errorf("findings after the race: %v", f)
	}
}

func TestAddLock(t *testing.T) {
	_, dir := decWS(t, decBase)
	lock := filepath.Join(dir, ".DECISIONS.md.lock")
	// a fresh lock of someone else: add gives up after the wait time, exit 2, register and lock untouched
	if err := os.WriteFile(lock, []byte("12345\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rc, _, errOut := runDayOut(t, t.TempDir(), []string{"DAY_LOCK_WAIT_MS=200"}, addArgs(dir, nil)...)
	if rc != 2 || !strings.Contains(errOut, "gesperrt") {
		t.Fatalf("held lock: rc %d, err %q", rc, errOut)
	}
	if readFile(t, filepath.Join(dir, decisionsFile)) != decBase || !exists(lock) {
		t.Error("register or foreign lock changed")
	}
	// a stale lock (older than 30 s: the writer died) is taken over and removed
	old := time.Now().Add(-time.Minute)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	if rc, _, errOut := runDayOut(t, t.TempDir(), nil, addArgs(dir, nil)...); rc != 0 {
		t.Fatalf("stale lock: rc %d, err %q", rc, errOut)
	}
	if exists(lock) {
		t.Error("the lock is not removed")
	}
}
