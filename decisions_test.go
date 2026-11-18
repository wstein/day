package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const decHead = "# Entscheidungsregister\n<!--\nFormat: - <ID> | <Datum> | ...\n-->\n"

const decBase = decHead +
	"- D-20261103-01 | 2026-11-03 | owner | decided | workspace | - | Erste. | [[Goals]]\n" +
	"- D-20261103-02 | 2026-11-03 | owner | superseded | workspace | - | Zweite; ersetzt durch D-20261104-01. | chat 2026-11-03\n" +
	"- D-20261104-01 | 2026-11-04 | owner | decided | day | D-20261103-02 | Dritte | mit | Pipe. | dayboard/LOGBOOK.md\n" +
	"- D-20261104-02 | 2026-11-04 | coordinator | proposed | global | - | Vierte. | chat 2026-11-04\n"

// decWS builds a workspace: <ws>/dayboard/{DAYBOARD.md,LOGBOOK.md}, <ws>/chartroom/notes/Goals.md. It returns the
// workspace and the dayboard directory.
func decWS(t *testing.T, register string) (ws, dir string) {
	t.Helper()
	ws, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir = filepath.Join(ws, "dayboard")
	notes := filepath.Join(ws, "chartroom", "notes")
	for _, d := range []string{dir, notes} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		filepath.Join(dir, "DAYBOARD.md"): template("2026-11-18"),
		filepath.Join(dir, "LOGBOOK.md"):  logbook,
		filepath.Join(notes, "Goals.md"):  "---\ntags: [\"type/decision\", \"status/decided\"]\n---\nx\n",
	}
	if register != "" {
		files[filepath.Join(dir, decisionsFile)] = register
	}
	for p, c := range files {
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return ws, dir
}

func TestDecisionsCheckValidRegister(t *testing.T) {
	ws, dir := decWS(t, decBase)
	f, st := checkDecisions(dir, filepath.Join(ws, "chartroom", "notes"), "2026-11-18")
	if len(f) != 0 {
		t.Fatalf("findings on a valid register: %v", f)
	}
	if st.Rows != 4 || st.Proposed != 1 || len(st.Stale) != 1 {
		t.Fatalf("stats = %+v, want 4 rows, 1 proposed, 1 stale", st)
	}
}

// A proposed row is stale when it is MORE than 7 days old (display only).
func TestDecisionsStaleBoundary(t *testing.T) {
	ws, dir := decWS(t, decBase)
	for today, want := range map[string]int{"2026-11-10": 0, "2026-11-11": 0, "2026-11-12": 1, "2026-11-04": 0} {
		_, st := checkDecisions(dir, filepath.Join(ws, "chartroom", "notes"), today)
		if len(st.Stale) != want {
			t.Errorf("today %s: %d stale rows, want %d", today, len(st.Stale), want)
		}
	}
}

func TestDecisionsCheckMissingFileIsFine(t *testing.T) {
	ws, dir := decWS(t, "")
	f, st := checkDecisions(dir, filepath.Join(ws, "chartroom", "notes"), "2026-11-18")
	if len(f) != 0 || st.Rows != 0 {
		t.Fatalf("missing file: %v %+v", f, st)
	}
}

func TestDecisionsCheckUnreadableIsFinding(t *testing.T) {
	ws, dir := decWS(t, "")
	if err := os.Mkdir(filepath.Join(dir, decisionsFile), 0o755); err != nil {
		t.Fatal(err)
	}
	f, _ := checkDecisions(dir, filepath.Join(ws, "chartroom", "notes"), "2026-11-18")
	if len(f) != 1 || !strings.Contains(f[0], "nicht lesbar") {
		t.Fatalf("directory instead of file: %v", f)
	}
}

// Each rule is broken once; exactly this finding (and no other) must appear, on the given line (0 = no line).
func TestDecisionsCheckCounterproofs(t *testing.T) {
	rep := func(old, new string) func(string) string {
		return func(s string) string {
			if !strings.Contains(s, old) {
				t.Fatalf("setup: %q not in base", old)
			}
			return strings.Replace(s, old, new, 1)
		}
	}
	chat := "| chat 2026-11-03\n"
	cases := []struct {
		name string
		mut  func(string) string
		want string
		line int
		more int // additional findings allowed (cascades)
	}{
		{"Feldzahl", rep(" | owner | decided | workspace | - | Erste.", " | decided | workspace | - | Erste."), "weniger als 8 Felder (7)", 5, 1},
		{"ID-Format", rep("D-20261103-01 | 2026", "D-2026113-01 | 2026"), "ungültige ID", 5, 1},
		{"doppelte ID", rep("D-20261103-02 | 2026-11-03", "D-20261103-01 | 2026-11-03"), "doppelte ID D-20261103-01", 6, 4},
		{"nicht aufsteigend", func(s string) string {
			l := strings.Split(s, "\n")
			l[4], l[5] = l[5], l[4]
			return strings.Join(l, "\n")
		}, "IDs nicht aufsteigend bei D-20261103-01", 6, 2},
		{"Lücke je Tag", rep("D-20261103-02 | 2026-11-03 | owner | superseded", "D-20261103-03 | 2026-11-03 | owner | superseded"), "NN von D-20261103-03 nicht lückenlos (erwartet 02)", 6, 2},
		{"ungültiges Datum", rep("| 2026-11-03 | owner | decided |", "| 2026-13-03 | owner | decided |"), "ungültiges Datum 2026-13-03", 5, 0},
		{"ID-Datum", rep("| 2026-11-03 | owner | decided |", "| 2026-11-04 | owner | decided |"), "ID-Datum ungleich Datumsfeld", 5, 0},
		{"Status", rep("| owner | decided | workspace | - | Erste", "| owner | agreed | workspace | - | Erste"), "ungültiger Status agreed", 5, 0},
		{"decided nicht von owner", rep("| owner | decided | workspace | - | Erste", "| agent:author | decided | workspace | - | Erste"), "decided braucht Entscheider owner, nicht agent:author", 5, 0},
		{"proposed vom owner", rep("| coordinator | proposed |", "| owner | proposed |"), "proposed braucht Entscheider coordinator oder agent:<rolle>, nicht owner", 8, 0},
		{"Geltung", rep("| decided | workspace | - | Erste", "| decided | Work Space | - | Erste"), "ungültige Geltung Work Space", 5, 0},
		{"Aussage leer", rep("- | Erste. |", "- |  |"), "leere Aussage", 5, 0},
		{"Ersetzt unbekannt", rep("| - | Erste.", "| D-20250101-01 | Erste."), "Ersetzt D-20250101-01 steht nicht früher in der Datei", 5, 0},
		{"Ersetzt spätere Zeile", rep("| - | Erste.", "| D-20261104-02 | Erste."), "Ersetzt D-20261104-02 steht nicht früher in der Datei", 5, 1},
		{"Ersetzt Format", rep("| D-20261103-02 | Dritte", "| D-2026 | Dritte"), "ungültiges Ersetzt D-2026", 7, 2},
		{"chat-Datum ungültig", rep("chat 2026-11-03\n", "chat 2026-02-30\n"), "Quelle: chat mit ungültigem Datum 2026-02-30", 6, 0},
		{"chat-Datum abweichend", rep("chat 2026-11-03\n", "chat 2026-11-02\n"), "Quelle: chat-Datum 2026-11-02 ungleich Zeilendatum 2026-11-03", 6, 0},
		{"Notiz fehlt", rep("[[Goals]]", "[[Keine Notiz]]"), "Quelle: Chartroom-Notiz `Keine Notiz` nicht gefunden", 5, 0},
		{"Pfad fehlt", rep("dayboard/LOGBOOK.md", "dayboard/NOPE.md"), "Quelle: Pfad `dayboard/NOPE.md` nicht gefunden", 7, 0},
		{"Notiz mit ..", rep("[[Goals]]", "[[../../dayboard/LOGBOOK]]"), "Quelle: Chartroom-Notiz `../../dayboard/LOGBOOK` nicht gefunden", 5, 0},
		{"Notiz ../x", rep("[[Goals]]", "[[../x]]"), "Quelle: Chartroom-Notiz `../x` nicht gefunden", 5, 0},
		{"Pfad mit ..", rep("dayboard/LOGBOOK.md", "dayboard/../dayboard/LOGBOOK.md"), "Quelle: ungültige Quelle `dayboard/../dayboard/LOGBOOK.md`", 7, 0},
		{"absoluter Pfad", rep("dayboard/LOGBOOK.md", "/etc/passwd"), "Quelle: ungültige Quelle `/etc/passwd`", 7, 0},
		{"Quelle sonst", rep("[[Goals]]", "irgendwo"), "Quelle: ungültige Quelle `irgendwo`", 5, 0},
		{"superseded ohne Nachfolger im Text", rep("Zweite; ersetzt durch D-20261104-01.", "Zweite."), "Aussage von D-20261103-02 nennt den Nachfolger nicht (`ersetzt durch D-20261104-01`)", 6, 0},
		{"superseded ohne ersetzende Zeile", rep("| D-20261103-02 | Dritte", "| - | Dritte"), "superseded-Zeile D-20261103-02 hat keinen Nachfolger", 6, 0},
		{"ersetzte Zeile nicht superseded", rep("owner | superseded | workspace | - | Zweite", "owner | decided | workspace | - | Zweite"), "ersetzte Zeile D-20261103-02 hat Status decided, nicht superseded", 7, 0},
		{"Kommentar fehlt", rep("<!--\nFormat: - <ID> | <Datum> | ...\n-->\n", ""), "Kommentarblock fehlt", 0, 0},
		{"Kommentar offen", rep("-->\n", "\n"), "Kommentarblock ab Zeile 2 nicht geschlossen", 0, 0},
		{"Titel fehlt", rep("# Entscheidungsregister\n", "Entscheidungsregister\n"), "Titelzeile `# ...` fehlt", 1, 0},
		{"Fließtext", func(s string) string { return s + "nur Text\n" }, "Zeile beginnt nicht mit `- `", 9, 0},
		{"CR", func(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }, "CR gefunden", 0, 0},
		{"Leerzeichen am Ende", rep(chat, "| chat 2026-11-03 \n"), "Leerzeichen am Zeilenende oder Tab", 6, 0},
		{"Tab in der Aussage", rep("Erste.", "Er\tste."), "Leerzeichen am Zeilenende oder Tab", 5, 0},
		{"Steuerzeichen", rep("Erste.", "Er\x1bste."), "Steuer- oder Formatzeichen", 5, 0},
		{"Bidi", rep("Erste.", "Er\u202este."), "Steuer- oder Formatzeichen", 5, 0},
		{"Feld mit Rand-Leerzeichen", rep("| Erste. |", "|  Erste. |"), "Feld mit Leerzeichen am Rand", 5, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ws, dir := decWS(t, c.mut(decBase))
			f, _ := checkDecisions(dir, filepath.Join(ws, "chartroom", "notes"), "2026-11-18")
			var hit string
			for _, x := range f {
				if strings.Contains(x, c.want) {
					hit = x
				}
			}
			if hit == "" {
				t.Fatalf("no finding %q in %q", c.want, f)
			}
			if c.line > 0 && !strings.Contains(hit, "Zeile "+strconv.Itoa(c.line)+":") {
				t.Errorf("finding %q is not on line %d", hit, c.line)
			}
			if !strings.HasPrefix(hit, decisionsFile+": ") {
				t.Errorf("finding %q lacks the file prefix", hit)
			}
			if len(f) > 1+c.more {
				t.Errorf("too many findings (%d): %q", len(f), f)
			}
		})
	}
}

// runDayOut is runDay with stdout and stderr (the test binary is the day binary).
func runDayOut(t *testing.T, dir string, env []string, args ...string) (int, string, string) {
	t.Helper()
	c := exec.Command(os.Args[0], append([]string{"-test.run=TestHelperProcess", "--"}, args...)...)
	c.Dir = dir
	c.Env = append(os.Environ(), "DAY_HELPER=1", "DAY_DIR=", "DAY_GIT_NAME=Tester", "DAY_GIT_EMAIL=t@example.invalid")
	c.Env = append(c.Env, env...)
	var so, se strings.Builder
	c.Stdout, c.Stderr = &so, &se
	rc := 0
	if err := c.Run(); err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatal(err)
		}
		rc = ee.ExitCode()
	}
	return rc, so.String(), se.String()
}

func TestCheckCommandRegisterFindingsAndSummary(t *testing.T) {
	_, dir := decWS(t, decBase)
	rc, out, _ := runDayOut(t, t.TempDir(), nil, "check", "--dir", dir, "--date", "2026-11-18")
	if rc != 0 {
		t.Fatalf("valid register: rc %d, out %q", rc, out)
	}
	if want := "Entscheidungsregister: 4 Zeilen, 1 proposed, 1 älter als 7 Tage\n"; !strings.Contains(out, want) {
		t.Fatalf("summary %q missing in %q", want, out)
	}
	if !strings.Contains(out, "D-20261104-02") {
		t.Errorf("stale proposed row not listed: %q", out)
	}
	bad := strings.Replace(decBase, "| decided | day |", "| agreed | day |", 1)
	if err := os.WriteFile(filepath.Join(dir, decisionsFile), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	rc, out, _ = runDayOut(t, t.TempDir(), nil, "check", "--dir", dir, "--date", "2026-11-18")
	if rc != 1 || !strings.Contains(out, "DECISIONS.md: Zeile 7: ungültiger Status agreed") {
		t.Fatalf("bad status: rc %d, out %q", rc, out)
	}
}

func TestCheckCommandWithoutRegisterIsSilent(t *testing.T) {
	_, dir := decWS(t, "")
	rc, out, _ := runDayOut(t, t.TempDir(), nil, "check", "--dir", dir, "--date", "2026-11-18")
	if rc != 0 || out != "" {
		t.Fatalf("no register: rc %d, out %q", rc, out)
	}
}

func TestCheckCommandChartroomOption(t *testing.T) {
	ws, dir := decWS(t, decBase)
	if err := os.Rename(filepath.Join(ws, "chartroom", "notes"), filepath.Join(ws, "elsewhere")); err != nil {
		t.Fatal(err)
	}
	rc, out, _ := runDayOut(t, t.TempDir(), nil, "check", "--dir", dir, "--date", "2026-11-18")
	if rc != 1 || !strings.Contains(out, "Chartroom-Notiz `Goals` nicht gefunden") {
		t.Fatalf("default chartroom must be <workspace>/chartroom/notes: rc %d, out %q", rc, out)
	}
	rc, out, _ = runDayOut(t, t.TempDir(), nil, "check", "--dir", dir, "--date", "2026-11-18", "--chartroom", filepath.Join(ws, "elsewhere"))
	if rc != 0 {
		t.Fatalf("--chartroom: rc %d, out %q", rc, out)
	}
}

func TestHelpListsDecisions(t *testing.T) {
	rc, out, _ := runDayOut(t, t.TempDir(), nil, "--help")
	if rc != 0 || !strings.Contains(out, "decisions") || !strings.Contains(out, "{init,rollover,check,briefing,decisions}") {
		t.Fatalf("help: rc %d, out %q", rc, out)
	}
}
