// decisions: the decision register <dayboard>/DECISIONS.md (check, list, from-chartroom, add).
//
// One row per decision: - <ID> | <JJJJ-MM-TT> | <Entscheider> | <Status> | <Geltung> | <Ersetzt> | <Aussage> | <Quelle>
// The statement may contain " | "; the source is the part after the LAST " | ". The comment block at the top of the
// file is the format specification for people; this file is the specification for the tool.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
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
