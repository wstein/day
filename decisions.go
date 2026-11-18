// decisions: the decision register <dayboard>/DECISIONS.md (check, list, from-chartroom, add).
//
// One row per decision: - <ID> | <JJJJ-MM-TT> | <Entscheider> | <Status> | <Geltung> | <Ersetzt> | <Aussage> | <Quelle>
// The statement may contain " | "; the source is the part after the LAST " | ". The comment block at the top of the
// file is the format specification for people; this file is the specification for the tool.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	decisionsFile      = "DECISIONS.md"
	decisionsStaleDays = 7
)

// decRow is one register line; Line is the 1-based line number in the file.
type decRow struct {
	Line                                                int
	ID, Date, By, Status, Scope, Replaces, Text, Source string
}

// decStats is the display-only summary shown by check.
type decStats struct {
	Rows, Proposed int
	Stale          []string // proposed rows older than decisionsStaleDays days
}

var (
	decIDRe    = regexp.MustCompile(`^D-(\d{8})-(\d{2})$`)
	decDateRe  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	decScopeRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	decAgentRe = regexp.MustCompile(`^agent:[a-z0-9-]+$`)
	decChatRe  = regexp.MustCompile(`^chat (\S+)$`)
	decNoteRe  = regexp.MustCompile(`^\[\[(.+)\]\]$`)
	decStatus  = map[string]bool{"proposed": true, "decided": true, "superseded": true, "revoked": true}
)

func validDate(s string) bool {
	if !decDateRe.MatchString(s) {
		return false
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

type decFinding struct {
	line int
	msg  string
}

// checkDecisionsText validates the register text: the rows found and the findings (with line numbers, ordered by line).
// workspace is the base of path sources, chartroom the folder of the notes for [[Titel]] sources.
func checkDecisionsText(text, workspace, chartroom, today string) (rows []decRow, findings []string) {
	var fs []decFinding
	bad := func(n int, why string) { fs = append(fs, decFinding{n, why}) }
	if strings.Contains(text, "\r") {
		bad(0, "CR gefunden")
		text = strings.ReplaceAll(text, "\r", "")
	}
	lines := strings.Split(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "# ") {
		bad(1, "Titelzeile `# ...` fehlt")
	}
	inComment, seenComment, commentFrom := false, false, 0
	byID := map[string]*decRow{}
	lastKey, haveLast := "", false
	dayCount := map[string]int{}
	for i, line := range lines {
		n := i + 1
		if inComment {
			if strings.Contains(line, "-->") {
				inComment = false
			}
			continue
		}
		if strings.HasPrefix(line, "<!--") {
			seenComment = true
			if !strings.Contains(line, "-->") {
				inComment, commentFrom = true, n
			}
			continue
		}
		if n == 1 || line == "" {
			continue
		}
		if line != strings.TrimRight(line, " \t") || strings.Contains(line, "\t") {
			bad(n, "Leerzeichen am Zeilenende oder Tab")
		}
		for _, c := range line {
			if c != '\t' && unicode.In(c, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp) {
				bad(n, "Steuer- oder Formatzeichen")
				break
			}
		}
		if !strings.HasPrefix(line, "- ") {
			bad(n, "Zeile beginnt nicht mit `- `")
			continue
		}
		parts := strings.Split(line[2:], " | ")
		if len(parts) < 8 {
			bad(n, fmt.Sprintf("weniger als 8 Felder (%d)", len(parts)))
			continue
		}
		for k, p := range parts {
			if (k < len(parts)-1 && p != strings.TrimSpace(p)) || (k == len(parts)-1 && p != strings.TrimLeft(p, " ")) {
				bad(n, "Feld mit Leerzeichen am Rand")
				break
			}
		}
		r := decRow{Line: n, ID: parts[0], Date: parts[1], By: parts[2], Status: parts[3], Scope: parts[4], Replaces: parts[5],
			Text: strings.Join(parts[6:len(parts)-1], " | "), Source: strings.TrimSpace(parts[len(parts)-1])}
		m := decIDRe.FindStringSubmatch(r.ID)
		if m == nil {
			bad(n, "ungültige ID "+r.ID)
			continue
		}
		day, nn := m[1], int(m[2][0]-'0')*10+int(m[2][1]-'0')
		switch {
		case !validDate(r.Date):
			bad(n, "ungültiges Datum "+r.Date)
		case day != strings.ReplaceAll(r.Date, "-", ""):
			bad(n, "ID-Datum ungleich Datumsfeld")
		}
		if byID[r.ID] != nil {
			bad(n, "doppelte ID "+r.ID)
		}
		key := fmt.Sprintf("%s-%02d", day, nn)
		if haveLast && key <= lastKey {
			bad(n, "IDs nicht aufsteigend bei "+r.ID)
		}
		lastKey, haveLast = key, true
		if exp := dayCount[day] + 1; nn != exp {
			bad(n, fmt.Sprintf("NN von %s nicht lückenlos (erwartet %02d)", r.ID, exp))
		}
		dayCount[day] = nn
		switch {
		case !decStatus[r.Status]:
			bad(n, "ungültiger Status "+r.Status)
		case r.Status == "proposed":
			if r.By != "coordinator" && !decAgentRe.MatchString(r.By) {
				bad(n, "proposed braucht Entscheider coordinator oder agent:<rolle>, nicht "+r.By)
			}
		case r.By != "owner":
			bad(n, r.Status+" braucht Entscheider owner, nicht "+r.By)
		}
		if !decScopeRe.MatchString(r.Scope) {
			bad(n, "ungültige Geltung "+r.Scope)
		}
		if r.Text == "" {
			bad(n, "leere Aussage")
		}
		if r.Replaces != "-" {
			if !decIDRe.MatchString(r.Replaces) {
				bad(n, "ungültiges Ersetzt "+r.Replaces)
			} else if byID[r.Replaces] == nil {
				bad(n, "Ersetzt "+r.Replaces+" steht nicht früher in der Datei")
			}
		}
		if why := decSourceFinding(r, workspace, chartroom); why != "" {
			bad(n, "Quelle: "+why)
		}
		rr := r
		if byID[r.ID] == nil {
			byID[r.ID] = &rr
		}
		rows = append(rows, r)
	}
	if inComment {
		bad(0, fmt.Sprintf("Kommentarblock ab Zeile %d nicht geschlossen", commentFrom))
	}
	if !seenComment {
		bad(0, "Kommentarblock fehlt")
	}
	// supersession: the replaced row is superseded and names its successor; every superseded row has a successor
	replaced := map[string]bool{}
	for _, r := range rows {
		old := byID[r.Replaces]
		if r.Replaces == "-" || old == nil {
			continue
		}
		replaced[r.Replaces] = true
		switch {
		case old.Status != "superseded":
			bad(r.Line, fmt.Sprintf("ersetzte Zeile %s hat Status %s, nicht superseded", old.ID, old.Status))
		case !strings.Contains(old.Text, "ersetzt durch "+r.ID):
			bad(old.Line, fmt.Sprintf("Aussage von %s nennt den Nachfolger nicht (`ersetzt durch %s`)", old.ID, r.ID))
		}
	}
	for _, r := range rows {
		if r.Status == "superseded" && !replaced[r.ID] {
			bad(r.Line, fmt.Sprintf("superseded-Zeile %s hat keinen Nachfolger", r.ID))
		}
	}
	sort.SliceStable(fs, func(i, j int) bool { return fs[i].line < fs[j].line })
	for _, f := range fs {
		if f.line > 0 {
			findings = append(findings, fmt.Sprintf("%s: Zeile %d: %s", decisionsFile, f.line, f.msg))
		} else {
			findings = append(findings, fmt.Sprintf("%s: %s", decisionsFile, f.msg))
		}
	}
	return rows, findings
}

// decSourceFinding returns what is wrong with the source of a row, or "".
func decSourceFinding(r decRow, workspace, chartroom string) string {
	src := r.Source
	if m := decChatRe.FindStringSubmatch(src); m != nil {
		switch {
		case !validDate(m[1]):
			return "chat mit ungültigem Datum " + m[1]
		case m[1] != r.Date:
			return fmt.Sprintf("chat-Datum %s ungleich Zeilendatum %s", m[1], r.Date)
		}
		return ""
	}
	if m := decNoteRe.FindStringSubmatch(src); m != nil {
		title := m[1]
		if strings.ContainsAny(title, "/\\\x00") || strings.Contains(title, "..") || !isFile(filepath.Join(chartroom, title+".md")) {
			return "Chartroom-Notiz `" + title + "` nicht gefunden"
		}
		return ""
	}
	if strings.Contains(src, "/") && !strings.HasPrefix(src, "/") && !strings.Contains(src, "\\") && !strings.Contains(src, "\x00") {
		for _, seg := range strings.Split(src, "/") {
			if seg == ".." {
				return "ungültige Quelle `" + src + "`"
			}
		}
		if !exists(filepath.Join(workspace, src)) {
			return "Pfad `" + src + "` nicht gefunden"
		}
		return ""
	}
	return "ungültige Quelle `" + src + "`"
}

func isFile(p string) bool { st, err := os.Stat(p); return err == nil && st.Mode().IsRegular() }

// checkDecisions validates dir/DECISIONS.md (a missing file is fine). The workspace is the parent of dir.
func checkDecisions(dir, chartroom, today string) (findings []string, st decStats) {
	b, err := os.ReadFile(filepath.Join(dir, decisionsFile))
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		return []string{decisionsFile + ": nicht lesbar: " + err.Error()}, st
	}
	rows, findings := checkDecisionsText(string(b), filepath.Dir(dir), chartroom, today)
	now, _ := time.Parse("2006-01-02", today)
	st.Rows = len(rows)
	for _, r := range rows {
		if r.Status != "proposed" {
			continue
		}
		st.Proposed++
		if d, err := time.Parse("2006-01-02", r.Date); err == nil && int(now.Sub(d).Hours()/24) > decisionsStaleDays {
			st.Stale = append(st.Stale, fmt.Sprintf("%s (%s): %s", r.ID, r.Date, mask(r.Text, 100, false)))
		}
	}
	return findings, st
}

// readRegister returns the text of dir/DECISIONS.md; a missing file is an error (exit 2).
func readRegister(dir string) string {
	p := filepath.Join(dir, decisionsFile)
	if !exists(p) {
		panic(die(decisionsFile + " fehlt: " + p))
	}
	return read(p)
}

// decJSONRow is the stable JSON shape of one row (schema_version 1). Replaces is null for "-".
type decJSONRow struct {
	Line     int     `json:"line"`
	ID       string  `json:"id"`
	Date     string  `json:"date"`
	By       string  `json:"by"`
	Status   string  `json:"status"`
	Scope    string  `json:"scope"`
	Replaces *string `json:"replaces"`
	Text     string  `json:"text"`
	Source   string  `json:"source"`
}

func cmdDecisions(a args) int {
	switch a.sub {
	case "open":
		a.status = "proposed"
		return decList(a)
	case "from-chartroom":
		return decFromChartroom(a)
	case "add":
		return decAdd(a)
	}
	return decList(a)
}

// decList prints the rows that pass the filters (status, scope, since), in file order.
func decList(a args) int {
	rows, findings := checkDecisionsText(readRegister(a.dir), filepath.Dir(a.dir), a.chartroom, a.date)
	if len(findings) > 0 {
		fmt.Fprintf(os.Stderr, "day: Warnung: %s hat %d Verstöße (day check zeigt sie)\n", decisionsFile, len(findings))
	}
	var sel []decRow
	for _, r := range rows {
		if (a.status == "" || r.Status == a.status) && (a.scope == "" || r.Scope == a.scope) && (a.since == "" || r.Date >= a.since) {
			sel = append(sel, r)
		}
	}
	if a.json {
		out := struct {
			SchemaVersion int          `json:"schema_version"`
			Decisions     []decJSONRow `json:"decisions"`
		}{1, []decJSONRow{}}
		for _, r := range sel {
			j := decJSONRow{r.Line, r.ID, r.Date, r.By, r.Status, r.Scope, nil, r.Text, r.Source}
			if r.Replaces != "-" {
				rep := r.Replaces
				j.Replaces = &rep
			}
			out.Decisions = append(out.Decisions, j)
		}
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			panic(die("JSON: " + err.Error()))
		}
		fmt.Print(b.String())
		return 0
	}
	if len(sel) == 0 {
		fmt.Println("keine Einträge")
		return 0
	}
	const f = "%-14s %-10s %-11s %-10s %-13s %s\n"
	fmt.Printf(f, "ID", "DATUM", "ENTSCHEIDER", "STATUS", "GELTUNG", "AUSSAGE")
	for _, r := range sel {
		fmt.Printf(f, mask(r.ID, 0, false), mask(r.Date, 0, false), mask(r.By, 0, false), mask(r.Status, 0, false), mask(r.Scope, 0, false), mask(r.Text, 100, false))
	}
	return 0
}

var decNoteDateRes = []*regexp.Regexp{
	regexp.MustCompile(`Decision of (\d{4}-\d{2}-\d{2})`),
	regexp.MustCompile(`Update (\d{4}-\d{2}-\d{2})`),
}

// noteFrontmatter splits a chartroom note into its tags (inline list or YAML list) and the text after the frontmatter.
// ok is false for a note without frontmatter.
func noteFrontmatter(text string) (tags []string, body string, ok bool) {
	lines := strings.Split(strings.ReplaceAll(text, "\r", ""), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, "", false
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return nil, "", false
	}
	clean := func(s string) string { return strings.Trim(strings.TrimSpace(s), `"'`) }
	for i := 1; i < end; i++ {
		rest, has := strings.CutPrefix(lines[i], "tags:")
		if !has {
			continue
		}
		rest = strings.TrimSpace(rest)
		if strings.HasPrefix(rest, "[") {
			for _, t := range strings.Split(strings.Trim(rest, "[] "), ",") {
				tags = append(tags, clean(t))
			}
			break
		}
		for j := i + 1; j < end; j++ {
			item, isItem := strings.CutPrefix(strings.TrimSpace(lines[j]), "- ")
			if !isItem {
				break
			}
			tags = append(tags, clean(item))
		}
		break
	}
	return tags, strings.Join(lines[end+1:], "\n"), true
}

// decCandidate is a proposed register row made from a chartroom note (Date "" = no date in the note).
type decCandidate struct {
	Title, Date, Text, ID string
}

// decCandidates returns the notes with the tags type/decision and status/decided whose title is no [[Titel]] source of
// a register row, with a proposed next free ID per day. Dated candidates come first (by date, then title).
func decCandidates(notesDir string, rows []decRow) []decCandidate {
	ents, err := os.ReadDir(notesDir)
	if err != nil {
		panic(die("Chartroom nicht lesbar: " + err.Error()))
	}
	named := map[string]bool{}
	next := map[string]int{}
	for _, r := range rows {
		if m := decNoteRe.FindStringSubmatch(r.Source); m != nil {
			named[m[1]] = true
		}
		if m := decIDRe.FindStringSubmatch(r.ID); m != nil {
			n, _ := strconv.Atoi(m[2])
			if n >= next[m[1]] {
				next[m[1]] = n + 1
			}
		}
	}
	var out []decCandidate
	for _, e := range ents {
		title, isMd := strings.CutSuffix(e.Name(), ".md")
		if !isMd || !e.Type().IsRegular() || named[title] {
			continue
		}
		b, err := os.ReadFile(filepath.Join(notesDir, e.Name()))
		if err != nil {
			continue
		}
		tags, body, ok := noteFrontmatter(string(b))
		if !ok || !containsStr(tags, "type/decision") || !containsStr(tags, "status/decided") {
			continue
		}
		c := decCandidate{Title: title, Text: "(kein Text)"}
		for _, l := range strings.Split(body, "\n") {
			if t := strings.TrimSpace(l); t != "" && !strings.HasPrefix(t, "#") {
				c.Text = mask(t, 160, false)
				break
			}
		}
	dates:
		for _, re := range decNoteDateRes {
			for _, m := range re.FindAllStringSubmatch(body, -1) {
				if validDate(m[1]) {
					c.Date = m[1]
					break dates
				}
			}
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Date == "") != (b.Date == "") {
			return a.Date != ""
		}
		if a.Date != b.Date {
			return a.Date < b.Date
		}
		return a.Title < b.Title
	})
	for i := range out {
		c := &out[i]
		if c.Date == "" {
			c.ID = "D-????????-??"
			continue
		}
		day := strings.ReplaceAll(c.Date, "-", "")
		n := next[day]
		if n == 0 {
			n = 1
		}
		c.ID = fmt.Sprintf("D-%s-%02d", day, n)
		next[day] = n + 1
	}
	return out
}

func containsStr(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// decFromChartroom prints proposed rows for decided chartroom notes that the register does not name. It writes nothing.
func decFromChartroom(a args) int {
	rows, _ := checkDecisionsText(readRegister(a.dir), filepath.Dir(a.dir), a.chartroom, a.date)
	cands := decCandidates(a.chartroom, rows)
	if len(cands) == 0 {
		fmt.Println("keine Kandidaten")
		return 0
	}
	fmt.Printf("Kandidaten (nur Vorschlag, nichts geschrieben): %d\n", len(cands))
	for _, c := range cands {
		date := c.Date
		if date == "" {
			date = "?"
			fmt.Printf("# Datum fehlt: [[%s]]\n", mask(c.Title, 0, false))
		}
		fmt.Printf("- %s | %s | coordinator | proposed | workspace | - | %s | [[%s]]\n", c.ID, date, c.Text, mask(c.Title, 0, false))
	}
	return 0
}

const (
	decLockFile  = ".DECISIONS.md.lock"
	decLockStale = 30 * time.Second
)

// lockRegister takes the lock file next to the register (O_EXCL) and returns the function that releases it. A lock
// older than decLockStale counts as left behind by a dead writer and is taken over. It waits DAY_LOCK_WAIT_MS
// (default 10000) milliseconds, then stops with exit 2. Known narrow window: two writers that find the same stale lock
// at the same moment can both take it over.
func lockRegister(dir string) func() {
	wait := 10 * time.Second
	if v, err := strconv.Atoi(os.Getenv("DAY_LOCK_WAIT_MS")); err == nil && v >= 0 {
		wait = time.Duration(v) * time.Millisecond
	}
	p := filepath.Join(dir, decLockFile)
	deadline := time.Now().Add(wait)
	for {
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			fmt.Fprintf(f, "%d\n", os.Getpid())
			f.Close()
			return func() { os.Remove(p) }
		}
		if !os.IsExist(err) {
			panic(die("nicht schreibbar: " + err.Error()))
		}
		if st, serr := os.Stat(p); serr == nil && time.Since(st.ModTime()) > decLockStale {
			os.Remove(p)
			continue
		}
		if time.Now().After(deadline) {
			panic(die(decisionsFile + " ist gesperrt: " + p + " (ein anderer day decisions add läuft; nach 30 s gilt die Sperre als verwaist)"))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// decInput refuses values that could break the line format: invalid UTF-8, newline, tab and other control, format and
// separator characters. " | " is allowed only in the statement.
func decInput(name, v string, pipeOK bool) {
	bad := !utf8.ValidString(v)
	for _, c := range v {
		bad = bad || unicode.In(c, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp)
	}
	if bad {
		panic(die("--" + name + ": Zeilenumbruch, Tab, Steuer- und Formatzeichen sind nicht erlaubt"))
	}
	if !pipeOK && strings.Contains(v, " | ") {
		panic(die("--" + name + ": ' | ' nicht erlaubt"))
	}
}

// nextDecisionID returns the next free ID of the day (the highest NN of that day plus one).
func nextDecisionID(rows []decRow, date string) string {
	day := strings.ReplaceAll(date, "-", "")
	n := 0
	for _, r := range rows {
		if m := decIDRe.FindStringSubmatch(r.ID); m != nil && m[1] == day {
			if v, _ := strconv.Atoi(m[2]); v > n {
				n = v
			}
		}
	}
	if n >= 99 {
		panic(die("mehr als 99 Entscheidungen an einem Tag: " + date))
	}
	return fmt.Sprintf("D-%s-%02d", day, n+1)
}

// decAdd appends one row. The whole new file is checked before it replaces the old one (one atomic write); with a
// finding the file stays as it was (exit 1). Nothing else is ever changed or deleted.
func decAdd(a args) int {
	decInput("by", a.by, false)
	decInput("scope", a.scope, false)
	decInput("source", a.source, false)
	decInput("text", a.text, true)
	unlock := lockRegister(a.dir)
	defer unlock()
	path := filepath.Join(a.dir, decisionsFile)
	text := readRegister(a.dir)
	rows, _ := checkDecisionsText(text, filepath.Dir(a.dir), a.chartroom, a.date)
	id := nextDecisionID(rows, a.date)
	row := fmt.Sprintf("- %s | %s | %s | %s | %s | - | %s | %s", id, a.date, a.by, a.status, a.scope, a.text, a.source)
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	text += row + "\n"
	if _, findings := checkDecisionsText(text, filepath.Dir(a.dir), a.chartroom, a.date); len(findings) > 0 {
		for _, f := range findings {
			fmt.Println(f)
		}
		return 1
	}
	if a.dry {
		fmt.Println("dry-run: " + row)
		return 0
	}
	writeAtomicTmp(path, []byte(text), ".DECISIONS.md.*.tmp")
	fmt.Println("angelegt: " + row)
	return 0
}
