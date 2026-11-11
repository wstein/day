// day: dayboard tool (init, rollover, check). Standard library only, no push, no remote.
//
// Model: the board folder (default ./dayboard in the MAIN checkout of the enclosing repo, also when run from a linked
// worktree) has no git repo of its own. BACKLOG.md and LOGBOOK.md are tracked by the enclosing (workspace) repo;
// DAYBOARD.md is ignored there and never committed. init/rollover commit only the files they changed among
// BACKLOG.md and LOGBOOK.md (git commit --only, so other staged files stay staged and uncommitted). Outside any repo
// the files are just written. A folder that still has its own .git (legacy) keeps committing into that nested repo
// and prints a migration hint.
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var secs = []string{"Jetzt", "Heute", "Wartet auf Owner", "Geparkt", "Erledigt heute"}
var limits = map[string]int{"Jetzt": 1, "Heute": 3, "Wartet auf Owner": 5}

const (
	begin   = "<!-- stw:begin -->"
	end     = "<!-- stw:end -->"
	maxLen  = 240
	logbook = "# Logbuch\n\nHinweis: append-only. Pro Tag ein Block `## JJJJ-MM-TT`, nichts Bestehendes ändern.\n"
	backlog = "# Backlog\n\nRegeln: eine Zeile pro Eintrag (Was, Fertig wenn, Ziel, Link). Nicht Jetzt/Heute; was dran ist, geht ins Dayboard.\n\n## Next\n\n## Later\n\n## Ideas\n"
)

var headRe = regexp.MustCompile(`^# Dayboard \(Stand (\d{4}-\d{2}-\d{2})\)$`)

type die string

func (d die) Error() string { return string(d) }

func template(day string) string {
	var b strings.Builder
	for _, n := range secs {
		fmt.Fprintf(&b, "\n## %s\n", n)
	}
	return fmt.Sprintf("# Dayboard (Stand %s)\n\nRegeln: eine Zeile pro Eintrag (Was, Fertig wenn, Ziel, Link)."+
		"\nJetzt max 1, Heute max 3, Wartet auf Owner max 5 Einträge.\n%s\n%s\n%s\n", day, b.String(), begin, end)
}

type result struct {
	rc       int
	out, err string
}

// git runs git in cwd. ident is a list of key=value pairs passed as -c options. With ok, a failure is returned, not fatal.
func git(cwd string, ident []string, ok bool, args ...string) result {
	full := []string{}
	for _, kv := range ident {
		full = append(full, "-c", kv)
	}
	full = append(full, args...)
	c := exec.Command("git", full...)
	c.Dir = cwd
	var so, se bytes.Buffer
	c.Stdout, c.Stderr = &so, &se
	err := c.Run()
	rc := 0
	if err != nil {
		if ee, isExit := err.(*exec.ExitError); isExit {
			rc = ee.ExitCode()
		} else {
			panic(die("git nicht ausführbar: " + err.Error()))
		}
	}
	if rc != 0 && !ok {
		panic(die(fmt.Sprintf("git %s: %s", args[0], strings.TrimSpace(se.String()))))
	}
	return result{rc, so.String(), strings.TrimSpace(se.String())}
}

func read(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		panic(die("nicht lesbar: " + err.Error()))
	}
	return string(b)
}

func write(path, text string, appendMode bool) {
	flag := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if appendMode {
		flag = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	}
	f, err := os.OpenFile(path, flag, 0o644)
	if err == nil {
		_, err = f.WriteString(text)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		panic(die("nicht schreibbar: " + err.Error()))
	}
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }
func isDir(p string) bool  { st, err := os.Stat(p); return err == nil && st.IsDir() }

// sections maps a section name to [start, end) of its body; the end is the next heading or facts marker.
// Like the original, a repeated heading keeps its first entry (and closes that one).
func sections(lines []string) map[string]*[2]int {
	out := map[string]*[2]int{}
	cur := ""
	for i, l := range lines {
		if cur != "" && (strings.HasPrefix(l, "## ") || strings.TrimSpace(l) == begin || strings.TrimSpace(l) == end) {
			out[cur][1] = i
			cur = ""
		}
		if strings.HasPrefix(l, "## ") {
			cur = strings.TrimSpace(l[3:])
			if _, have := out[cur]; !have {
				out[cur] = &[2]int{i + 1, len(lines)}
			}
		}
	}
	return out
}

// identityOpt (legacy repos) returns the commit identity from DAY_GIT_NAME/DAY_GIT_EMAIL or the global git config, or nil.
func identityOpt() []string {
	n, e := os.Getenv("DAY_GIT_NAME"), os.Getenv("DAY_GIT_EMAIL")
	if n == "" {
		n = strings.TrimSpace(git(".", nil, true, "config", "--global", "--get", "user.name").out)
	}
	if e == "" {
		e = strings.TrimSpace(git(".", nil, true, "config", "--global", "--get", "user.email").out)
	}
	if n == "" || e == "" {
		return nil
	}
	return []string{"user.name=" + n, "user.email=" + e}
}

// envIdent returns -c pairs only for the DAY_GIT_NAME/DAY_GIT_EMAIL values that are set; git resolves the rest.
func envIdent() []string {
	var id []string
	if n := os.Getenv("DAY_GIT_NAME"); n != "" {
		id = append(id, "user.name="+n)
	}
	if e := os.Getenv("DAY_GIT_EMAIL"); e != "" {
		id = append(id, "user.email="+e)
	}
	return id
}

const legacyHint = "Hinweis: %s hat ein eigenes .git (veraltet). Migration: dayboard/.git wegverschieben, dann im Workspace `/dayboard/` aus .git/info/exclude und eine Zeile `dayboard/` aus .gitignore entfernen, `git add dayboard/BACKLOG.md dayboard/LOGBOOK.md` ausführen und `dayboard/DAYBOARD.md` in .gitignore eintragen."

// target says where a commit for board dir d goes: legacy (nested repo in d), enclosing repo (cwd = nearest existing
// ancestor of d, inside a repo), or none.
type target struct {
	legacy, enclosing bool
	cwd               string
}

func findTarget(d string) target {
	if exists(filepath.Join(d, ".git")) {
		return target{legacy: true, cwd: d}
	}
	anc := d
	for !isDir(anc) {
		anc = filepath.Dir(anc)
	}
	if top := git(anc, nil, true, "rev-parse", "--show-toplevel"); top.rc == 0 && strings.TrimSpace(top.out) != "" {
		return target{enclosing: true, cwd: anc}
	}
	return target{}
}

func warn(f string, a ...any) { fmt.Fprintf(os.Stderr, "day: Warnung: "+f+"\n", a...) }

// busy reports a rebase, merge, cherry-pick, revert or sequencer operation in progress in the repo around cwd.
func busy(cwd string) string {
	for _, n := range []string{"rebase-merge", "rebase-apply", "MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "sequencer"} {
		p := strings.TrimSpace(git(cwd, nil, true, "rev-parse", "--path-format=absolute", "--git-path", n).out)
		if p != "" && exists(p) {
			return n
		}
	}
	return ""
}

func verb(paths []string) string {
	if len(paths) > 1 {
		return "bleiben"
	}
	return "bleibt"
}

// commit commits the given files (relative to d). Legacy: into the nested repo, as before. Enclosing: only these
// paths (git commit --only), failures only warn and leave the index as it was. None: nothing.
func commit(t target, d, msg string, paths []string) bool {
	switch {
	case t.legacy:
		ident := identityOpt()
		if ident == nil {
			panic(die("Git-Identität fehlt: DAY_GIT_NAME/DAY_GIT_EMAIL setzen oder global user.name/user.email konfigurieren"))
		}
		git(d, nil, false, append([]string{"add", "--"}, paths...)...)
		git(d, ident, false, append([]string{"commit", "-q", "-m", msg, "--"}, paths...)...)
		return true
	case t.enclosing:
		if len(paths) == 0 {
			return false
		}
		list := strings.Join(paths, ", ")
		if b := busy(d); b != "" {
			warn("Repo mitten in %s, kein Commit; %s %s geändert", b, list, verb(paths))
			return false
		}
		// Remember whether the index already differed for these paths, so a failure restores it exactly.
		clean := git(d, nil, true, append([]string{"diff", "--cached", "--quiet", "--"}, paths...)...).rc == 0
		if r := git(d, nil, true, append([]string{"add", "--"}, paths...)...); r.rc != 0 {
			detail := r.err
			for _, p := range paths {
				if ci := strings.TrimSpace(git(d, nil, true, "check-ignore", "-v", "--", p).out); ci != "" {
					detail += "\n  ignoriert von: " + ci
				}
			}
			warn("git add fehlgeschlagen, kein Commit; %s %s geändert\n%s", list, verb(paths), detail)
			return false
		}
		if r := git(d, envIdent(), true, append([]string{"commit", "--only", "-q", "-m", msg, "--"}, paths...)...); r.rc != 0 {
			if clean {
				if u := git(d, nil, true, append([]string{"reset", "-q", "--"}, paths...)...); u.rc != 0 {
					git(d, nil, true, append([]string{"rm", "--cached", "-q", "--"}, paths...)...)
				}
			}
			warn("Commit fehlgeschlagen, %s %s geändert (Index zurückgesetzt)\n%s", list, verb(paths), r.err)
			return false
		}
		return true
	}
	return false
}

// realpath resolves symlinks of the longest existing prefix, like Python's os.path.realpath.
func realpath(p string) string {
	p, _ = filepath.Abs(p)
	rest := ""
	cur := p
	for {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

type args struct {
	cmd, dir, date, seed string
	dry                  bool
}

func cmdInit(a args) int {
	d, dry := a.dir, a.dry
	pre := ""
	if dry {
		pre = "dry-run: "
	}
	seed, haveSeed := "", a.seed != ""
	if haveSeed {
		seed = read(a.seed)
	}
	t := findTarget(d)
	if t.legacy {
		if identityOpt() == nil && !dry {
			panic(die("Git-Identität fehlt: DAY_GIT_NAME/DAY_GIT_EMAIL setzen oder global user.name/user.email konfigurieren"))
		}
		fmt.Printf(legacyHint+"\n", d)
	} else if t.enclosing && git(t.cwd, nil, true, "check-ignore", "-q", "--", filepath.Join(d, "DAYBOARD.md")).rc == 1 {
		rel := "dayboard/DAYBOARD.md"
		if top := strings.TrimSpace(git(t.cwd, nil, true, "rev-parse", "--show-toplevel").out); top != "" {
			if r, err := filepath.Rel(realpath(top), filepath.Join(d, "DAYBOARD.md")); err == nil {
				rel = filepath.ToSlash(r)
			}
		}
		fmt.Printf("Hinweis: %s ist im Repo nicht ignoriert; in .gitignore eintragen (wird nie committet).\n", rel)
	}
	if !dry {
		if err := os.MkdirAll(d, 0o755); err != nil {
			panic(die("nicht schreibbar: " + err.Error()))
		}
	}
	board := template(a.date)
	if haveSeed {
		board = seed
	}
	names := []string{"DAYBOARD.md", "LOGBOOK.md", "BACKLOG.md"}
	texts := []string{board, logbook, backlog}
	var created []string
	for i, name := range names {
		p := filepath.Join(d, name)
		if exists(p) {
			fmt.Printf("%s: vorhanden, unverändert\n", name)
			continue
		}
		fmt.Printf("%s%s: angelegt\n", pre, name)
		created = append(created, name)
		if !dry {
			write(p, texts[i], false)
		}
	}
	// Commit set: enclosing repo only BACKLOG/LOGBOOK that were created; legacy all existing files, as before.
	var paths []string
	for _, n := range names {
		if t.legacy && exists(filepath.Join(d, n)) {
			paths = append(paths, n)
		} else if t.enclosing && n != "DAYBOARD.md" {
			isCreated := false
			for _, c := range created {
				isCreated = isCreated || c == n
			}
			// also files left untracked by an earlier failed commit
			if isCreated || (exists(filepath.Join(d, n)) && git(d, nil, true, "ls-files", "--error-unmatch", "--", n).rc != 0) {
				paths = append(paths, n)
			}
		}
	}
	legacyNeeds := t.legacy && (len(created) > 0 || dry || git(d, nil, true, "rev-parse", "--verify", "-q", "HEAD").rc != 0)
	if dry {
		if (t.enclosing && len(paths) > 0) || (legacyNeeds && len(created) > 0) {
			fmt.Println("dry-run: Commit 'dayboard: init'")
		}
		return 0
	}
	if t.legacy && !legacyNeeds {
		return 0
	}
	if commit(t, d, "dayboard: init", paths) {
		fmt.Println("Commit: dayboard: init")
	}
	return 0
}

func cmdRollover(a args) int {
	d := a.dir
	bp, lp := filepath.Join(d, "DAYBOARD.md"), filepath.Join(d, "LOGBOOK.md")
	lines := strings.Split(read(bp), "\n")
	m := headRe.FindStringSubmatch(strings.TrimRight(lines[0], "\r"))
	if m == nil {
		panic(die("Kopfzeile fehlt oder ist ungültig: " + bp))
	}
	old := m[1]
	if old == a.date {
		fmt.Println("nothing to roll over")
		return 0
	}
	t := findTarget(d)
	if t.legacy && !a.dry && identityOpt() == nil {
		panic(die("Git-Identität fehlt: DAY_GIT_NAME/DAY_GIT_EMAIL setzen oder global user.name/user.email konfigurieren"))
	}
	secMap := sections(lines)
	se, have := secMap["Erledigt heute"]
	if !have {
		panic(die("Abschnitt 'Erledigt heute' fehlt"))
	}
	s, e := se[0], se[1]
	var done []string
	for _, l := range lines[s:e] {
		if strings.TrimSpace(l) != "" {
			done = append(done, l)
		}
	}
	var span []string
	if len(done) == 0 {
		span = lines[s:e]
	} else if strings.TrimSpace(lines[e-1]) == "" {
		span = []string{""}
	}
	nw := append(append(append([]string{}, lines[:s]...), span...), lines[e:]...)
	nw[0] = "# Dayboard (Stand " + a.date + ")"
	log := logbook
	if exists(lp) {
		log = read(lp)
	}
	if len(done) > 0 {
		sep := "\n\n"
		if strings.HasSuffix(log, "\n\n") {
			sep = ""
		} else if strings.HasSuffix(log, "\n") {
			sep = "\n"
		}
		log += fmt.Sprintf("%s## %s\n%s\n", sep, old, strings.Join(done, "\n"))
	}
	if a.dry {
		c := ""
		if t.legacy || (t.enclosing && len(done) > 0) {
			c = fmt.Sprintf(", Commit 'dayboard: %s'", old)
		}
		fmt.Printf("dry-run: %d Zeilen unter '## %s' an LOGBOOK.md, Kopfzeile -> %s%s\n", len(done), old, a.date, c)
		return 0
	}
	write(bp, strings.Join(nw, "\n"), false)
	if t.legacy || len(done) > 0 || exists(lp) {
		write(lp, log, false)
	}
	if t.legacy {
		fmt.Printf(legacyHint+"\n", d)
		commit(t, d, "dayboard: "+old, []string{"DAYBOARD.md", "LOGBOOK.md"})
	} else if len(done) > 0 {
		commit(t, d, "dayboard: "+old, []string{"LOGBOOK.md"})
	}
	fmt.Printf("rolled over %s: %d Zeilen ins Logbuch, Stand %s\n", old, len(done), a.date)
	return 0
}

const (
	ledgerFile = "debt-ledger.md"
	ledgerDays = 3
)

var (
	ledgerRe = regexp.MustCompile(`^- (\d{4}-\d{2}-\d{2}) \| ([^|]*[^| ]) \| ([^|]*[^| ]) \| ([^|]*[^| ])$`)
	hex7Re   = regexp.MustCompile(`(^|[^0-9A-Za-z])[0-9a-f]{7}($|[^0-9A-Za-z])`)
)

// checkLedger validates dir/debt-ledger.md (a missing file is fine). Allowed lines: blank, "#" headings, an HTML
// comment block, and entries "- JJJJ-MM-TT | Datei:Zeile | Fix | Quelle" (the source holds a 7-char head). It returns
// the findings (with line numbers), the number of open entries and the entries older than ledgerDays days.
func checkLedger(dir, today string) (findings []string, open int, old []string) {
	b, err := os.ReadFile(filepath.Join(dir, ledgerFile))
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		return []string{ledgerFile + ": nicht lesbar: " + err.Error()}, 0, nil
	}
	now, _ := time.Parse("2006-01-02", today)
	inComment := false
	for i, l := range strings.Split(string(b), "\n") {
		l = strings.TrimRight(l, "\r")
		bad := func(why string) {
			findings = append(findings, fmt.Sprintf("%s: Zeile %d: %s", ledgerFile, i+1, why))
		}
		t := strings.TrimSpace(l)
		if inComment {
			inComment = !strings.Contains(t, "-->")
			continue
		}
		switch {
		case t == "" || strings.HasPrefix(t, "#"):
		case strings.HasPrefix(t, "<!--"):
			inComment = !strings.Contains(t, "-->")
		default:
			m := ledgerRe.FindStringSubmatch(l)
			if m == nil {
				bad("kein Eintrag `- JJJJ-MM-TT | Datei:Zeile | Fix | Quelle`")
				continue
			}
			d, err := time.Parse("2006-01-02", m[1])
			switch {
			case err != nil:
				bad("ungültiges Datum " + m[1])
			case !hex7Re.MatchString(m[4]):
				bad("Quelle ohne 7-stelligen Head")
			case d.After(now):
				bad("Datum " + m[1] + " liegt nach heute " + today)
			default:
				open++
				if int(now.Sub(d).Hours()/24) > ledgerDays {
					old = append(old, l)
				}
			}
		}
	}
	return
}

func cmdCheck(a args) int {
	lines := strings.Split(read(filepath.Join(a.dir, "DAYBOARD.md")), "\n")
	var f []string
	m := headRe.FindStringSubmatch(strings.TrimRight(lines[0], "\r"))
	if m == nil {
		f = append(f, "Kopfzeile: fehlt oder ist ungültig")
	} else if m[1] != a.date {
		f = append(f, fmt.Sprintf("Kopfzeile: Datum %s ungleich heute %s", m[1], a.date))
	}
	sm := sections(lines)
	for _, n := range secs {
		se, have := sm[n]
		if !have {
			f = append(f, n+": Abschnitt fehlt")
			continue
		}
		entries := 0
		for i := se[0]; i < se[1]; i++ {
			l := lines[i]
			if strings.TrimSpace(l) == "" {
				continue
			}
			if strings.HasPrefix(l, "- ") {
				entries++
			} else {
				f = append(f, fmt.Sprintf("%s: Zeile %d ist kein einzeiliger Eintrag", n, i+1))
			}
			if c := utf8.RuneCountInString(l); c > maxLen {
				f = append(f, fmt.Sprintf("%s: Zeile %d hat %d Zeichen (max %d)", n, i+1, c, maxLen))
			}
		}
		if lim, has := limits[n]; has && entries > lim {
			f = append(f, fmt.Sprintf("%s: %d Einträge (max %d)", n, entries, lim))
		}
	}
	var b, en []int
	for i, l := range lines {
		switch strings.TrimSpace(l) {
		case begin:
			b = append(b, i)
		case end:
			en = append(en, i)
		}
	}
	switch {
	case len(b) == 0 && len(en) == 0:
		f = append(f, "Fakten-Block: Markierungen fehlen")
	case len(b) == 0 || len(en) == 0:
		miss := end
		if len(b) == 0 {
			miss = begin
		}
		f = append(f, "Fakten-Block: "+miss+" fehlt")
	case len(b) > 1 || len(en) > 1 || b[0] > en[0]:
		f = append(f, "Fakten-Block: Markierungen doppelt oder in falscher Reihenfolge")
	}
	lf, open, old := checkLedger(a.dir, a.date)
	f = append(f, lf...)
	for _, x := range f {
		fmt.Println(x)
	}
	// Display only, never a finding: the ledger does not change the exit code (except for invalid lines above).
	if open > 0 {
		fmt.Printf("Schuldenbuch: %d offen, %d älter als %d Tage\n", open, len(old), ledgerDays)
		for _, l := range old {
			fmt.Println("  " + l)
		}
	}
	if len(f) > 0 {
		return 1
	}
	return 0
}

// mainCheckout returns the main checkout of the repo around the current directory: the parent of the git common dir
// when that is a ".git" directory (so also from inside a linked worktree). Otherwise (submodule, separate git dir,
// bare) it falls back to --show-toplevel; empty when not in a work tree.
func mainCheckout() string {
	if out, err := exec.Command("git", "rev-parse", "--path-format=absolute", "--git-common-dir").Output(); err == nil {
		if cd := strings.TrimSpace(string(out)); filepath.Base(cd) == ".git" {
			p := filepath.Dir(cd)
			// only when the parent really is a work tree (not e.g. a bare proj/.git with linked worktrees)
			if o, err := exec.Command("git", "-C", p, "rev-parse", "--show-toplevel").Output(); err == nil &&
				realpath(strings.TrimSpace(string(o))) == realpath(p) {
				return p
			}
		}
	}
	if out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output(); err == nil {
		return strings.TrimSpace(string(out))
	}
	return ""
}

// resolveDir: --dir, then $DAY_DIR, then ./dayboard in the main checkout of the current repo; no fixed path.
// The git default must already exist unless the board is to be created (init).
func resolveDir(given string, create bool) string {
	d := given
	if d == "" {
		d = os.Getenv("DAY_DIR")
	}
	if d == "" {
		if top := mainCheckout(); top != "" && (create || isDir(filepath.Join(top, "dayboard"))) {
			d = filepath.Join(top, "dayboard")
		}
	}
	if d == "" {
		if create {
			fmt.Fprintln(os.Stderr, "day: kein Dayboard-Verzeichnis: --dir oder $DAY_DIR angeben (oder in einem Git-Repository ausführen, dann wird ./dayboard im Haupt-Checkout des Git-Repos angelegt)")
		} else {
			fmt.Fprintln(os.Stderr, "day: kein Dayboard-Verzeichnis: --dir oder $DAY_DIR angeben (oder ./dayboard im Haupt-Checkout des Git-Repos)")
		}
		os.Exit(2)
	}
	if d == "~" || strings.HasPrefix(d, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			d = h + d[1:]
		}
	}
	return realpath(d)
}

const usage = `usage: day [-h] {init,rollover,check} ...

  init       Verzeichnis, DAYBOARD/LOGBOOK/BACKLOG.md anlegen [--dir D] [--date JJJJ-MM-TT] [--seed FILE] [--dry-run]
  rollover   Tageswechsel ins Logbuch             [--dir D] [--date JJJJ-MM-TT] [--dry-run]
  check      Verstöße melden (Exit 1 bei Funden)  [--dir D] [--date JJJJ-MM-TT]
             prüft auch debt-ledger.md (falls vorhanden) und zeigt Einträge älter als 3 Tage an (nur Anzeige)

--dir: Dayboard-Verzeichnis (Default $DAY_DIR, sonst ./dayboard im Haupt-Checkout des Git-Repos; init legt es an)
--date: Datum JJJJ-MM-TT (Default heute, zum Testen)
`

func argErr(msg string) {
	fmt.Fprintf(os.Stderr, "usage: day [-h] {init,rollover,check} ...\nday: error: %s\n", msg)
	os.Exit(2)
}

func parse(argv []string) args {
	var a args
	if len(argv) == 0 {
		argErr("the following arguments are required: cmd")
	}
	if argv[0] == "-h" || argv[0] == "--help" {
		fmt.Print(usage)
		os.Exit(0)
	}
	a.cmd = argv[0]
	if a.cmd != "init" && a.cmd != "rollover" && a.cmd != "check" {
		argErr(fmt.Sprintf("argument cmd: invalid choice: '%s' (choose from 'init', 'rollover', 'check')", a.cmd))
	}
	rest := argv[1:]
	for i := 0; i < len(rest); i++ {
		o, v, hasV := rest[i], "", false
		if strings.HasPrefix(o, "--") {
			if k := strings.Index(o, "="); k >= 0 {
				o, v, hasV = o[:k], o[k+1:], true
			}
		}
		need := func() string {
			if hasV {
				return v
			}
			if i+1 >= len(rest) {
				argErr("argument " + o + ": expected one argument")
			}
			i++
			return rest[i]
		}
		switch {
		case o == "-h" || o == "--help":
			fmt.Print(usage)
			os.Exit(0)
		case o == "--dir":
			a.dir = need()
		case o == "--date":
			a.date = need()
			if _, err := time.Parse("2006-01-02", a.date); err != nil {
				argErr("argument --date: JJJJ-MM-TT erwartet: " + a.date)
			}
		case o == "--seed" && a.cmd == "init":
			a.seed = need()
		case o == "--dry-run" && a.cmd != "check":
			a.dry = true
		default:
			argErr("unrecognized arguments: " + rest[i])
		}
	}
	return a
}

func run(argv []string) (rc int) {
	a := parse(argv)
	if a.date == "" {
		a.date = time.Now().Format("2006-01-02")
	}
	a.dir = resolveDir(a.dir, a.cmd == "init")
	defer func() {
		if r := recover(); r != nil {
			d, ok := r.(die)
			if !ok {
				panic(r)
			}
			fmt.Fprintf(os.Stderr, "day: %s\n", string(d))
			rc = 2
		}
	}()
	switch a.cmd {
	case "init":
		return cmdInit(a)
	case "rollover":
		return cmdRollover(a)
	}
	return cmdCheck(a)
}

func main() { os.Exit(run(os.Args[1:])) }
