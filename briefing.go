// briefing: `day briefing` collects the facts of `stw status --json` into its own marked block of DAYBOARD.md (between
// <!-- stw:begin --> and <!-- stw:end -->) and prints a short overview. The planning sections of the board belong to the
// coordinator and are never touched. Everything read from the JSON is untrusted text: control characters are masked,
// lengths are bounded and "<", ">" are written as entities, so no name, title or message can forge a marker or a heading.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	stwSchema    = 2                // the only schema_version of stw status --json that day reads
	stwHeading   = "## stw-Fakten"  // heading inside the markers
	stwTimeout   = 60 * time.Second // limit for the stw run; $DAY_STW_TIMEOUT_SEC overrides it (tests)
	stwMaxBytes  = 16 << 20
	briefingTopN = 10 // action items shown
	briefingRepo = 60 // count lines shown
	nameMax      = 40
	textMax      = 120
)

// ---- untrusted text ----

// mask returns s cut to max runes ("…" marks a cut), with control, format (bidi, zero width) and line/paragraph
// separator characters written visibly (\n, \x1b, U+202E). With md, "<" and ">" become entities.
func mask(s string, max int, md bool) string {
	s = strings.ToValidUTF8(s, "�")
	r := []rune(s)
	cut := false
	if max > 0 && len(r) > max {
		r, cut = r[:max], true
	}
	var b strings.Builder
	for _, c := range r {
		switch {
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\r':
			b.WriteString(`\r`)
		case c == '\t':
			b.WriteString(`\t`)
		case unicode.In(c, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp):
			if c < 0x100 {
				fmt.Fprintf(&b, `\x%02x`, c)
			} else {
				fmt.Fprintf(&b, `\u%04x`, c)
			}
		case md && c == '<':
			b.WriteString("&lt;")
		case md && c == '>':
			b.WriteString("&gt;")
		default:
			b.WriteRune(c)
		}
	}
	if cut {
		b.WriteString("…")
	}
	return b.String()
}

func safe(s string, max int) string { return mask(s, max, true) }

// ---- the JSON of stw status (schema 2) ----

type stwSrc struct {
	State     string `json:"state"`
	Open      *int   `json:"open"`
	Bot       *int   `json:"bot"`
	Bug       *int   `json:"bug"`
	Truncated bool   `json:"truncated"`
}

type stwCounts struct {
	PRs      *stwSrc `json:"pull_requests"`
	Issues   *stwSrc `json:"issues"`
	Branches *struct {
		Local  *int `json:"local"`
		Remote *int `json:"remote"`
	} `json:"branches"`
	Worktrees *int `json:"worktrees"`
	Stash     *int `json:"stash"`
}

type stwRepo struct {
	Name         *string    `json:"name"`
	Attention    *bool      `json:"attention"`
	Reasons      []string   `json:"reasons"`
	Error        *string    `json:"error"`
	Counts       *stwCounts `json:"counts"`
	PullRequests *struct {
		Error *string `json:"error"`
	} `json:"pull_requests"`
}

// stwAction and stwNotice only exist in the Go engine's schema 2 (the Rust stw has no such arrays and derives both).
type stwAction struct {
	Priority string `json:"priority"`
	Category string `json:"category"`
	Repo     string `json:"repo"`
	Title    string `json:"title"`
}

type stwNotice struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type stwDoc struct {
	Command *string    `json:"command"`
	Repos   *[]stwRepo `json:"repos"`
	Summary *struct {
		Total     *int `json:"total"`
		Repos     *int `json:"repos"`
		Attention *int `json:"attention"`
	} `json:"summary"`
	Totals     *stwCounts   `json:"totals"`
	Github     *bool        `json:"github"`
	GithubNote *string      `json:"github_note"`
	Actions    *[]stwAction `json:"actions"`
	Notices    *[]stwNotice `json:"notices"`
}

func (r stwRepo) name() string { return *r.Name }

// parseStw checks the schema version and the required fields; the error text is the reason shown to the owner.
func parseStw(data []byte) (*stwDoc, error) {
	var top map[string]json.RawMessage
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("leere Ausgabe")
	}
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, errors.New("kein gültiges JSON-Objekt: " + mask(err.Error(), 100, true))
	}
	v, ok := top["schema_version"]
	if !ok {
		return nil, fmt.Errorf("schema_version fehlt (erwartet %d)", stwSchema)
	}
	if string(bytes.TrimSpace(v)) != strconv.Itoa(stwSchema) {
		return nil, fmt.Errorf("schema_version %s gelesen, %d erwartet", mask(string(bytes.TrimSpace(v)), 20, true), stwSchema)
	}
	var d stwDoc
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, errors.New("JSON passt nicht zum Schema: " + mask(err.Error(), 100, true))
	}
	if d.Command == nil || *d.Command != "status" {
		return nil, errors.New(`Pflichtfeld command fehlt oder ist nicht "status"`)
	}
	if d.Repos == nil {
		return nil, errors.New("Pflichtfeld repos fehlt")
	}
	if d.Summary == nil {
		return nil, errors.New("Pflichtfeld summary fehlt")
	}
	for i, r := range *d.Repos {
		if r.Name == nil {
			return nil, fmt.Errorf("Pflichtfeld repos[%d].name fehlt", i)
		}
	}
	return &d, nil
}

// ---- rendering of the block ----

func numText(n *int) string {
	if n == nil || *n < 0 {
		return "?"
	}
	return strconv.Itoa(*n)
}

// srcText mirrors the text of stw status: "n+" is a lower bound, "?" is not readable, "n/a" is not read. Never 0 for
// something that was not read.
func srcText(label string, s *stwSrc, sub string) string {
	if s == nil {
		return label + " fehlt"
	}
	switch s.State {
	case "ok":
		if s.Open == nil || *s.Open < 0 {
			return label + " ?"
		}
		plus := ""
		if s.Truncated {
			plus = "+"
		}
		n := s.Bot
		if sub == "bug" {
			n = s.Bug
		}
		return fmt.Sprintf("%s %s%s (%s %s%s)", label, numText(s.Open), plus, sub, numText(n), plus)
	case "not-read", "no-remote":
		return label + " n/a"
	}
	return label + " ?"
}

func countsText(c *stwCounts) string {
	br := "Branches fehlt"
	if c.Branches != nil {
		br = fmt.Sprintf("Branches %s/%s", numText(c.Branches.Local), numText(c.Branches.Remote))
	}
	return strings.Join([]string{srcText("PRs", c.PRs, "bot"), srcText("Issues", c.Issues, "bug"), br,
		"Worktrees " + numText(c.Worktrees), "Stash " + numText(c.Stash)}, " | ")
}

func attention(r stwRepo) bool {
	if r.Attention != nil {
		return *r.Attention
	}
	return len(r.Reasons) > 0
}

var prioRank = map[string]int{"red": 0, "yellow": 1, "green": 2, "blue": 3, "violet": 4}

// actionItems returns the lines of the action list: the Go engine's actions by priority (stable), otherwise the
// repositories with an attention reason in the order of the JSON (the Rust stw has no priority).
func actionItems(d *stwDoc) []string {
	var out []string
	if d.Actions != nil {
		acts := append([]stwAction(nil), *d.Actions...)
		rank := func(p string) int {
			if r, ok := prioRank[p]; ok {
				return r
			}
			return len(prioRank)
		}
		sort.SliceStable(acts, func(i, j int) bool { return rank(acts[i].Priority) < rank(acts[j].Priority) })
		for _, a := range acts {
			out = append(out, fmt.Sprintf("%s %s %s: %s", safe(a.Priority, 10), safe(a.Category, 30), safe(a.Repo, nameMax), safe(a.Title, textMax)))
		}
		return out
	}
	for _, r := range *d.Repos {
		if !attention(r) {
			continue
		}
		var rs []string
		for _, x := range r.Reasons {
			rs = append(rs, safe(x, 30))
		}
		out = append(out, safe(r.name(), nameMax)+": "+strings.Join(rs, ", "))
	}
	return out
}

func noticeItems(d *stwDoc) []string {
	var out []string
	if d.Notices != nil {
		for _, n := range *d.Notices {
			out = append(out, safe(n.Code, 30)+": "+safe(n.Message, textMax))
		}
		return out
	}
	if d.GithubNote != nil && *d.GithubNote != "" {
		out = append(out, safe(*d.GithubNote, textMax))
	}
	for _, r := range *d.Repos {
		n := safe(r.name(), nameMax)
		switch {
		case r.PullRequests != nil && r.PullRequests.Error != nil:
			out = append(out, n+": pull requests not read ("+safe(*r.PullRequests.Error, textMax)+")")
		}
		if r.Error == nil && r.Counts != nil && r.Counts.Issues != nil && r.Counts.Issues.State == "failed" {
			out = append(out, n+": issues not read")
		}
	}
	return out
}

func more(n int) string { return fmt.Sprintf("- … und %d weitere (stw status)", n) }

// renderStw returns the lines between the markers (heading included).
func renderStw(d *stwDoc, stand string) []string {
	repos := *d.Repos
	out := []string{stwHeading, "", fmt.Sprintf("Stand %s, Quelle: stw status --json (schema %d)", stand, stwSchema), "", "Zähler:"}
	haveCounts := d.Totals != nil
	for _, r := range repos {
		haveCounts = haveCounts || r.Counts != nil
	}
	if !haveCounts {
		out = append(out, "- keine Zähler im JSON")
	} else {
		for i, r := range repos {
			if i == briefingRepo {
				out = append(out, more(len(repos)-i))
				break
			}
			if r.Counts == nil {
				out = append(out, "- "+safe(r.name(), nameMax)+": keine Zähler im JSON")
			} else {
				out = append(out, "- "+safe(r.name(), nameMax)+": "+countsText(r.Counts))
			}
		}
		if d.Totals == nil {
			out = append(out, "- All: keine Zähler im JSON")
		} else {
			out = append(out, "- All: "+countsText(d.Totals))
		}
	}
	items := actionItems(d)
	out = append(out, "", fmt.Sprintf("Handlungspunkte (%d):", len(items)))
	if len(items) == 0 {
		out = append(out, "- keine")
	}
	for i, it := range items {
		if i == briefingTopN {
			out = append(out, more(len(items)-i))
			break
		}
		out = append(out, "- "+it)
	}
	notes := noticeItems(d)
	out = append(out, "", fmt.Sprintf("Hinweise (%d):", len(notes)))
	if len(notes) == 0 {
		out = append(out, "- keine")
	}
	for _, n := range notes {
		out = append(out, "- "+n)
	}
	total, att := len(repos), len(items)
	if d.Summary.Total != nil {
		total = *d.Summary.Total
	} else if d.Summary.Repos != nil {
		total = *d.Summary.Repos
	}
	if d.Summary.Attention != nil {
		att = *d.Summary.Attention
	}
	sum := fmt.Sprintf("%d Repos, nichts braucht Aufmerksamkeit", total)
	if att > 0 {
		sum = fmt.Sprintf("%d von %d Repos brauchen Aufmerksamkeit", att, total)
	}
	if d.Github != nil && !*d.Github {
		sum += "; GitHub nicht gelesen"
	}
	return append(out, "", "Summary: "+sum)
}

// ---- running stw ----

type limitBuf struct {
	buf  bytes.Buffer
	max  int
	over bool
}

func (l *limitBuf) Write(p []byte) (int, error) {
	if l.buf.Len()+len(p) > l.max {
		l.over = true
		return 0, errors.New("Ausgabe zu groß")
	}
	return l.buf.Write(p)
}

// stwEnv is the environment of the stw run: a fixed list of variables (no shell, no GIT_*), colors off.
func stwEnv() []string {
	keep := []string{"PATH", "HOME", "USER", "LOGNAME", "TMPDIR", "LANG", "LC_ALL", "LC_CTYPE", "XDG_CONFIG_HOME",
		"XDG_DATA_HOME", "XDG_CACHE_HOME", "GH_TOKEN", "GITHUB_TOKEN", "GH_CONFIG_DIR", "SSH_AUTH_SOCK",
		"HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "https_proxy", "http_proxy", "no_proxy"}
	env := []string{"NO_COLOR=1"}
	for _, k := range keep {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}

func oneLine(s string, max int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return mask(s, max, true)
}

// runStw runs `stw status --json [--offline]` from PATH (no shell). stw exits 1 and still prints its JSON when a
// repository cannot be read locally; exit1 reports that case so the caller may still use a valid document.
func runStw(offline bool) (out []byte, exit1 bool, err error) {
	path, lerr := exec.LookPath("stw")
	if lerr != nil {
		return nil, false, errors.New("stw nicht im PATH gefunden")
	}
	limit := stwTimeout
	if s, e := strconv.Atoi(os.Getenv("DAY_STW_TIMEOUT_SEC")); e == nil && s > 0 {
		limit = time.Duration(s) * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	argv := []string{"status", "--json"}
	if offline {
		argv = append(argv, "--offline")
	}
	c := exec.CommandContext(ctx, path, argv...)
	c.Env = stwEnv()
	so, se := &limitBuf{max: stwMaxBytes}, &limitBuf{max: 64 << 10}
	c.Stdout, c.Stderr = so, se
	c.WaitDelay = 2 * time.Second
	rerr := c.Run()
	switch {
	case ctx.Err() != nil:
		return nil, false, fmt.Errorf("Zeitlimit von %d s überschritten", int(limit.Seconds()))
	case so.over:
		return nil, false, fmt.Errorf("Ausgabe größer als %d MiB", stwMaxBytes>>20)
	case rerr == nil:
		return so.buf.Bytes(), false, nil
	}
	var ee *exec.ExitError
	if errors.As(rerr, &ee) {
		msg := fmt.Sprintf("stw endete mit Exit %d", ee.ExitCode())
		if l := oneLine(se.buf.String(), textMax); l != "" {
			msg += ": " + l
		}
		return so.buf.Bytes(), ee.ExitCode() == 1, errors.New(msg)
	}
	return nil, false, errors.New("stw nicht ausführbar: " + oneLine(rerr.Error(), textMax))
}

func readLimited(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, stwMaxBytes+1))
	if err == nil && len(b) > stwMaxBytes {
		err = fmt.Errorf("Eingabe größer als %d MiB", stwMaxBytes>>20)
	}
	return b, err
}

// loadStw reads the JSON from the file, from stdin ("-") or from a stw run. A file that cannot be read is a usage
// error (panic die); everything else comes back as an error: stw is not available.
func loadStw(a args) (*stwDoc, error) {
	var data []byte
	switch a.file {
	case "-":
		b, err := readLimited(os.Stdin)
		if err != nil {
			return nil, errors.New("Standardeingabe nicht lesbar: " + oneLine(err.Error(), textMax))
		}
		data = b
	case "":
		out, exit1, err := runStw(a.offline)
		if err != nil {
			if exit1 {
				if d, perr := parseStw(out); perr == nil {
					return d, nil
				}
			}
			return nil, err
		}
		data = out
	default:
		f, err := os.Open(a.file)
		if err != nil {
			panic(die("Eingabedatei nicht lesbar: " + err.Error()))
		}
		defer f.Close()
		b, err := readLimited(f)
		if err != nil {
			panic(die("Eingabedatei nicht lesbar: " + err.Error()))
		}
		data = b
	}
	return parseStw(data)
}

// ---- the board ----

// replaceBlock returns the board text with the lines between the markers (bi/ei are 0-based line indexes of the markers)
// replaced, or, without markers (bi < 0), with a new marked block appended once.
func replaceBlock(lines []string, bi, ei int, block []string) string {
	if bi < 0 {
		for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
			lines = lines[:len(lines)-1]
		}
		lines = append(append(append(append([]string{}, lines...), "", begin), block...), end, "")
		return strings.Join(lines, "\n")
	}
	out := append(append(append([]string{}, lines[:bi+1]...), block...), lines[ei:]...)
	return strings.Join(out, "\n")
}

// markStale adds "veraltet seit <Zeitpunkt>: <Grund>" to the Stand line of an old block (once: an existing note stays).
func markStale(block []string, now, reason string) []string {
	out := append([]string{}, block...)
	for i, l := range out {
		if strings.HasPrefix(l, "Stand ") {
			if !strings.Contains(l, "; veraltet seit ") {
				out[i] = l + "; veraltet seit " + now + ": " + safe(reason, textMax)
			}
			break
		}
	}
	return out
}

// writeAtomic writes data next to path (temp file in the same directory, then rename); a symlink is followed.
func writeAtomic(path string, data []byte) {
	real := realpath(path)
	mode := os.FileMode(0o644)
	if st, err := os.Stat(real); err == nil {
		mode = st.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(real), ".DAYBOARD.md.*.tmp")
	if err != nil {
		panic(die("nicht schreibbar: " + err.Error()))
	}
	fail := func(err error) {
		tmp.Close()
		os.Remove(tmp.Name())
		panic(die("nicht schreibbar: " + err.Error()))
	}
	if _, err := tmp.Write(data); err != nil {
		fail(err)
	}
	if err := tmp.Chmod(mode); err != nil {
		fail(err)
	}
	if err := tmp.Sync(); err != nil {
		fail(err)
	}
	if err := tmp.Close(); err != nil {
		fail(err)
	}
	if err := os.Rename(tmp.Name(), real); err != nil {
		os.Remove(tmp.Name())
		panic(die("nicht schreibbar: " + err.Error()))
	}
}

// overview prints Jetzt/Heute/Wartet auf Owner in short form (the text up to "; Fertig wenn") and the ledger count.
func overview(lines []string, dir, today string) {
	sm := sections(lines)
	fmt.Println("Plan (DAYBOARD.md):")
	for _, n := range []string{"Jetzt", "Heute", "Wartet auf Owner"} {
		se, have := sm[n]
		if !have {
			fmt.Printf("%s: Abschnitt fehlt\n", n)
			continue
		}
		var items []string
		for i := se[0]; i < se[1]; i++ {
			if l := lines[i]; strings.HasPrefix(l, "- ") {
				short := l[2:]
				if k := strings.Index(short, "; Fertig wenn"); k >= 0 {
					short = short[:k]
				}
				items = append(items, mask(short, 110, false))
			}
		}
		if len(items) == 0 {
			fmt.Printf("%s: leer\n", n)
			continue
		}
		fmt.Printf("%s (%d):\n", n, len(items))
		for _, it := range items {
			fmt.Println("  - " + it)
		}
	}
	lf, open, old := checkLedger(dir, today)
	extra := ""
	if len(lf) > 0 {
		extra = fmt.Sprintf(" (%d ungültige Zeilen, siehe day check)", len(lf))
	}
	fmt.Printf("Schuldenbuch: %d offen, %d älter als %d Tage%s\n", open, len(old), ledgerDays, extra)
}

func cmdBriefing(a args) int {
	bp := filepath.Join(a.dir, "DAYBOARD.md")
	lines := strings.Split(read(bp), "\n")
	b, en := findMarkers(lines)
	bi, ei := -1, -1
	if len(b) > 0 || len(en) > 0 {
		if f := markerFindings(b, en); f != nil {
			panic(die("DAYBOARD.md: " + strings.Join(f, "; ") + " (nichts geschrieben)"))
		}
		bi, ei = b[0], en[0]
	}
	now := a.date + " " + a.time
	doc, err := loadStw(a)
	var block []string
	var failLine string
	switch {
	case err == nil:
		block = renderStw(doc, now)
	case bi >= 0:
		failLine = "stw status nicht verfügbar: " + safe(err.Error(), textMax)
		block = markStale(lines[bi+1:ei], now, err.Error())
	default:
		failLine = "stw status nicht verfügbar: " + safe(err.Error(), textMax)
	}
	wrote := ""
	if block != nil && !a.print {
		text := replaceBlock(lines, bi, ei, block)
		if text != strings.Join(lines, "\n") {
			writeAtomic(bp, []byte(text))
			wrote = "DAYBOARD.md aktualisiert"
		}
	}
	overview(lines, a.dir, a.date)
	fmt.Println()
	if failLine != "" {
		fmt.Println(failLine)
		fmt.Println()
	}
	for _, l := range block {
		fmt.Println(l)
	}
	if wrote != "" {
		fmt.Println("\n" + wrote)
	}
	return 0
}
