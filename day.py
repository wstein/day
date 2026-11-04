"""day: Dayboard-Werkzeug (init, rollover, check). Nur Standardbibliothek, kein Push, kein Remote."""
import argparse, datetime, os, re, subprocess, sys

SECS = ["Jetzt", "Heute", "Wartet auf Owner", "Geparkt", "Erledigt heute"]
LIMITS = {"Jetzt": 1, "Heute": 3, "Wartet auf Owner": 5}
BEGIN, END = "<!-- stw:begin -->", "<!-- stw:end -->"
HEAD = re.compile(r"^# Dayboard \(Stand (\d{4}-\d{2}-\d{2})\)$")
MAXLEN = 240
LOGBOOK = "# Logbuch\n\nHinweis: append-only. Pro Tag ein Block `## JJJJ-MM-TT`, nichts Bestehendes ändern.\n"


class Die(Exception):
    pass


def template(day):
    body = "".join("\n## %s\n" % n for n in SECS)
    return ("# Dayboard (Stand %s)\n\nRegeln: eine Zeile pro Eintrag (Was, Fertig wenn, Ziel, Link)."
            "\nJetzt max 1, Heute max 3, Wartet auf Owner max 5 Einträge.\n%s\n%s\n%s\n" % (day, body, BEGIN, END))


def git(cwd, *args, ident=None, ok=False):
    cmd = ["git"] + (["-c", "user.name=" + ident[0], "-c", "user.email=" + ident[1]] if ident else []) + list(args)
    try:
        p = subprocess.run(cmd, cwd=cwd, capture_output=True, text=True)
    except OSError as e:
        raise Die("git nicht ausführbar: %s" % e)
    if p.returncode and not ok:
        raise Die("git %s: %s" % (args[0], p.stderr.strip()))
    return p


def identity():
    n, e = os.environ.get("DAY_GIT_NAME"), os.environ.get("DAY_GIT_EMAIL")
    n = n or git(".", "config", "--global", "--get", "user.name", ok=True).stdout.strip()
    e = e or git(".", "config", "--global", "--get", "user.email", ok=True).stdout.strip()
    if not (n and e):
        raise Die("Git-Identität fehlt: DAY_GIT_NAME/DAY_GIT_EMAIL setzen oder global user.name/user.email konfigurieren")
    return n, e


def read(path):
    try:
        with open(path, encoding="utf-8", newline="") as f:
            return f.read()
    except OSError as e:
        raise Die("nicht lesbar: %s" % e)


def write(path, text, mode="w"):
    with open(path, mode, encoding="utf-8", newline="") as f:
        f.write(text)


def sections(lines):
    """Abschnittsname -> [Start, Ende) des Rumpfs; Ende ist die nächste Überschrift oder Fakten-Markierung."""
    out, cur = {}, None
    for i, l in enumerate(lines):
        if cur and (l.startswith("## ") or l.strip() in (BEGIN, END)):
            out[cur][1], cur = i, None
        if l.startswith("## "):
            cur = l[3:].strip()
            out.setdefault(cur, [i + 1, len(lines)])
    return out


def commit(d, msg, paths, ident):
    git(d, "add", "--", *paths)
    git(d, "commit", "-q", "-m", msg, "--", *paths, ident=ident)


def cmd_init(a):
    d, dry = a.dir, a.dry_run
    pre = "dry-run: " if dry else ""
    ident = None if dry else identity()
    seed = read(a.seed) if a.seed else None
    anc = os.path.dirname(d)
    while not os.path.isdir(anc):
        anc = os.path.dirname(anc)
    top = git(anc, "rev-parse", "--show-toplevel", ok=True)
    if top.returncode == 0 and top.stdout.strip():
        top = top.stdout.strip()
        line = "/" + os.path.relpath(d, os.path.realpath(top)) + "/"
        ex = git(top, "rev-parse", "--git-path", "info/exclude").stdout.strip()
        ex = ex if os.path.isabs(ex) else os.path.join(top, ex)
        cur = read(ex) if os.path.exists(ex) else ""
        if line in cur.splitlines():
            print("exclude: %s schon in %s" % (line, ex))
        else:
            print("%sexclude: %s in %s eingetragen (innerhalb des Repos %s)" % (pre, line, ex, top))
            if not dry:
                os.makedirs(os.path.dirname(ex), exist_ok=True)
                write(ex, ("" if cur == "" or cur.endswith("\n") else "\n") + line + "\n", "a")
    new = not os.path.exists(os.path.join(d, ".git"))
    if not dry:
        os.makedirs(d, exist_ok=True)
        if new:
            git(d, "init", "-q", "-b", "main")
    if new:
        print("%sRepo angelegt: %s (lokal, ohne Remote)" % (pre, d))
    files = {"DAYBOARD.md": seed if seed is not None else template(a.date), "LOGBOOK.md": LOGBOOK}
    created = []
    for name, text in files.items():
        p = os.path.join(d, name)
        if os.path.exists(p):
            print("%s: vorhanden, unverändert" % name)
            continue
        print("%s%s: angelegt" % (pre, name))
        created.append(name)
        if not dry:
            write(p, text)
    if dry:
        if created or new:
            print("dry-run: Commit 'dayboard: init'")
    elif created or git(d, "rev-parse", "--verify", "-q", "HEAD", ok=True).returncode != 0:
        commit(d, "dayboard: init", [n for n in files if os.path.exists(os.path.join(d, n))], ident)
        print("Commit: dayboard: init")
    return 0


def cmd_rollover(a):
    d, bp, lp = a.dir, os.path.join(a.dir, "DAYBOARD.md"), os.path.join(a.dir, "LOGBOOK.md")
    lines = read(bp).split("\n")
    m = HEAD.match(lines[0].rstrip("\r"))
    if not m:
        raise Die("Kopfzeile fehlt oder ist ungültig: %s" % bp)
    old = m.group(1)
    if old == a.date:
        print("nothing to roll over")
        return 0
    if not os.path.exists(os.path.join(d, ".git")):
        raise Die("kein Dayboard-Repo: %s" % d)
    ident = None if a.dry_run else identity()
    if "Erledigt heute" not in sections(lines):
        raise Die("Abschnitt 'Erledigt heute' fehlt")
    s, e = sections(lines)["Erledigt heute"]
    done = [l for l in lines[s:e] if l.strip()]
    span = lines[s:e] if not done else ([""] if not lines[e - 1].strip() else [])
    new = lines[:s] + span + lines[e:]
    new[0] = "# Dayboard (Stand %s)" % a.date
    log = read(lp) if os.path.exists(lp) else LOGBOOK
    if done:
        sep = "" if log.endswith("\n\n") else ("\n" if log.endswith("\n") else "\n\n")
        log += "%s## %s\n%s\n" % (sep, old, "\n".join(done))
    if a.dry_run:
        print("dry-run: %d Zeilen unter '## %s' an LOGBOOK.md, Kopfzeile -> %s, Commit 'dayboard: %s'"
              % (len(done), old, a.date, old))
        return 0
    write(bp, "\n".join(new))
    write(lp, log)
    commit(d, "dayboard: " + old, ["DAYBOARD.md", "LOGBOOK.md"], ident)
    print("rolled over %s: %d Zeilen ins Logbuch, Stand %s" % (old, len(done), a.date))
    return 0


def cmd_check(a):
    lines = read(os.path.join(a.dir, "DAYBOARD.md")).split("\n")
    f, m = [], HEAD.match(lines[0].rstrip("\r"))
    if not m:
        f.append("Kopfzeile: fehlt oder ist ungültig")
    elif m.group(1) != a.date:
        f.append("Kopfzeile: Datum %s ungleich heute %s" % (m.group(1), a.date))
    secs = sections(lines)
    for n in SECS:
        if n not in secs:
            f.append("%s: Abschnitt fehlt" % n)
            continue
        entries = 0
        for i, l in enumerate(lines[secs[n][0]:secs[n][1]], secs[n][0]):
            if not l.strip():
                continue
            entries += l.startswith("- ")
            if not l.startswith("- "):
                f.append("%s: Zeile %d ist kein einzeiliger Eintrag" % (n, i + 1))
            if len(l) > MAXLEN:
                f.append("%s: Zeile %d hat %d Zeichen (max %d)" % (n, i + 1, len(l), MAXLEN))
        if n in LIMITS and entries > LIMITS[n]:
            f.append("%s: %d Einträge (max %d)" % (n, entries, LIMITS[n]))
    b = [i for i, l in enumerate(lines) if l.strip() == BEGIN]
    en = [i for i, l in enumerate(lines) if l.strip() == END]
    if not b and not en:
        f.append("Fakten-Block: Markierungen fehlen")
    elif not b or not en:
        f.append("Fakten-Block: %s fehlt" % (BEGIN if not b else END))
    elif len(b) > 1 or len(en) > 1 or b[0] > en[0]:
        f.append("Fakten-Block: Markierungen doppelt oder in falscher Reihenfolge")
    for x in f:
        print(x)
    return 1 if f else 0


def isodate(s):
    try:
        return datetime.date.fromisoformat(s).isoformat()
    except ValueError:
        raise argparse.ArgumentTypeError("JJJJ-MM-TT erwartet: %s" % s)


def resolve_dir(given):
    """--dir, then $DAY_DIR, then ./dayboard in the git top level of the current directory; no fixed path."""
    d = given or os.environ.get("DAY_DIR")
    if not d:
        r = subprocess.run(["git", "rev-parse", "--show-toplevel"], capture_output=True, text=True)
        top = r.stdout.strip() if r.returncode == 0 else ""
        if top and os.path.isdir(os.path.join(top, "dayboard")):
            d = os.path.join(top, "dayboard")
    if not d:
        print("day: kein Dayboard-Verzeichnis: --dir oder $DAY_DIR angeben (oder ./dayboard im Git-Toplevel)", file=sys.stderr)
        sys.exit(2)
    return os.path.realpath(os.path.expanduser(d))


def main(argv):
    common = argparse.ArgumentParser(add_help=False)
    common.add_argument("--dir", help="Dayboard-Verzeichnis (Default $DAY_DIR, sonst ./dayboard im Git-Toplevel)")
    common.add_argument("--date", type=isodate, help="Datum JJJJ-MM-TT (Default heute, zum Testen)")
    ap = argparse.ArgumentParser(prog="day")
    sub = ap.add_subparsers(dest="cmd", required=True)
    p = sub.add_parser("init", parents=[common], help="Verzeichnis, Repo, Dateien anlegen")
    p.add_argument("--seed", help="Datei als Start-DAYBOARD.md")
    p.add_argument("--dry-run", action="store_true")
    p = sub.add_parser("rollover", parents=[common], help="Tageswechsel ins Logbuch")
    p.add_argument("--dry-run", action="store_true")
    sub.add_parser("check", parents=[common], help="Verstöße melden (Exit 1 bei Funden)")
    a = ap.parse_args(argv)
    a.date = a.date or datetime.date.today().isoformat()
    a.dir = resolve_dir(a.dir)
    try:
        return {"init": cmd_init, "rollover": cmd_rollover, "check": cmd_check}[a.cmd](a)
    except Die as e:
        print("day: %s" % e, file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
