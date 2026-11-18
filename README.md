# day

Small dayboard tool: `init`, `rollover`, `check`, `briefing` and `decisions` for a dayboard folder (`DAYBOARD.md`, `LOGBOOK.md`, `BACKLOG.md`). The folder has no
git repository of its own. One static Go binary, standard library only, no Python, no push, no remote. Needs `git` at run time.

## Install

With mise (release binaries of the GitHub repository `wstein/day`):

    mise use -g github:wstein/day

In the whr dev container `whr-up` installs it like the other tools; a failed install only warns.

## Build

    go build -o day .          # CGO_ENABLED=0 for a static binary; the binary is git-ignored

## Run

    ./day --help
    DAY_DIR=/path/to/dayboard ./day check

The dayboard data folder is passed with `--dir` or `$DAY_DIR`. Default: `./dayboard` in the MAIN checkout of the
current git repository, also when `day` runs inside a linked `git worktree` (the parent of `git rev-parse
--git-common-dir`, if that parent is a work tree). Where the common dir is not `<x>/.git` (submodule, separate git dir, bare `proj/.git`) it falls back to the work tree
top level; otherwise an error, exit 2. `init` creates the folder if it does not exist yet, with `DAYBOARD.md`,
`LOGBOOK.md` and a `BACKLOG.md` template (existing files are left unchanged; `rollover`, `check` and `briefing` do not touch
`BACKLOG.md`); `rollover`, `check` and `briefing` need an existing board.

## Debt ledger

`<dir>/debt-ledger.md` (optional, tracked by the workspace repo) holds review nits, one line each:

    - YYYY-MM-DD | <file[:line]> | <fix in one sentence> | <source: review/branch and 7-char head>

Date and file are the first two fields (the line number is optional, a file without one is fine); the source is the part after the
last ` | `; the fix is everything in between and may contain `|` (for example a command in backticks). Allowed besides entries:
blank lines, `#` headings and HTML comment blocks (`<!-- ... -->`, text after `-->` is a finding). Trailing space, tab or CR,
leading indentation and a UTF-8 BOM are tolerated. Done = delete the line (history is in git).

`day check` reports an invalid line as a finding with its line number (exit 1): not an entry, bad date, date after today, source
without a 7-char hex head (a 40-char hash does not count), comment not closed. Otherwise display only, no gate: with at least one
valid entry `check` prints `Schuldenbuch: N offen, M älter als 3 Tage` and lists the entries older than 3 days. The file does not count
toward the section limits. `day` never writes it.

## Decision register

`<dir>/DECISIONS.md` (optional, tracked by the workspace repo) is the index of the owner's decisions, one row each:

    - <ID> | <YYYY-MM-DD> | <decider> | <status> | <scope> | <replaces> | <statement> | <source>

`ID` is `D-YYYYMMDD-NN` (the date of the row, NN from 01 without gaps per day, ascending in the file). Decider: `owner` for
`decided`, `superseded` and `revoked`; `coordinator` or `agent:<role>` for `proposed`. Scope: `global`, `workspace` or a project
name. `replaces` is an earlier ID or `-`. The statement may contain ` | `; the source is the part after the LAST ` | `:
`chat YYYY-MM-DD` (same date as the row), a path relative to the workspace (the parent of the dayboard folder; no `..`, not
absolute) or `[[Title]]` of a note `<chartroom>/<Title>.md`. A replaced row stays, has the status `superseded` and says
`ersetzt durch <ID>` in its statement; every `superseded` row has a successor.

`day check` validates the file when it exists (nothing otherwise): format, comment block, no CR, no trailing space or tab, no control
characters, ID format, uniqueness, order and gaps, date, status, decider, scope, statement, `replaces`, supersession and source.
Each violation is a finding with its line number (exit 1). Display only (no gate): `Entscheidungsregister: N Zeilen, P proposed, Q älter
als 7 Tage` and the proposed rows older than 7 days. `--chartroom DIR` sets the notes folder (default `<workspace>/chartroom/notes`).

    day decisions list [--status S] [--scope X] [--since YYYY-MM-DD] [--json]
    day decisions open [--scope X] [--since YYYY-MM-DD] [--json]     # = list --status proposed
    day decisions from-chartroom [--chartroom DIR]                   # read only
    day decisions add --by B --status proposed|decided --scope S --text T --source SRC [--date D] [--supersedes ID] [--dry-run]

`list` prints a table (fields masked) or, with `--json`, `{"schema_version": 1, "decisions": [...]}` with one object per row in file
order (`line`, `id`, `date`, `by`, `status`, `scope`, `replaces` (null for none), `text`, `source`). `from-chartroom` prints rows as
proposals (`proposed`, `coordinator`, scope `workspace`) for chartroom notes tagged `type/decision` and `status/decided` whose title
is no `[[Title]]` source of the register: the date comes from `Decision of YYYY-MM-DD` or `Update YYYY-MM-DD` in the note, `?` (and a
marker line) when there is none; the statement is the first text line, cut at 160 characters. It never writes.

`add` appends ONE row: the ID is the next free one of the date (default today), decided only with `--by owner`. The whole new file is
checked before anything is written; with a finding the file stays byte-identical (exit 1). The write is atomic (temp file in the same
directory, fsync, rename) under the lock file `.DECISIONS.md.lock` (a lock older than 30 s counts as left behind; waiting stops after 10 s,
`DAY_LOCK_WAIT_MS` changes that, exit 2). `--supersedes ID` marks the old row `superseded` with `; ersetzt durch <new ID>.` and puts
the old ID into `replaces` of the new row, in the same write; a proposal cannot replace a row. Values with newline, tab, control, format or
separator characters are refused (exit 2); ` | ` is allowed only in `--text`. No row is ever deleted and no row other than the replaced one is changed.

## briefing

`day briefing [FILE|-] [--print] [--offline] [--dir D] [--date YYYY-MM-DD] [--time HH:MM]` collects the facts of `stw status --json`
into its own block of `DAYBOARD.md` and prints an overview. The planning sections (Jetzt, Heute, Wartet auf Owner, ...) are never
touched. `stw status` does not know the dayboard; `day` reads the stw JSON with a fixed `schema_version` (only `2` is accepted).

- Input: `FILE` is read as the stw JSON, `-` reads standard input, without an argument `day` runs `stw status --json` itself: `stw` from
  `PATH`, no shell, 60 s limit (the whole process group is killed), working directory = the parent of the dayboard directory (the workspace;
  keep the board at `<workspace>/dayboard`, `--dir` or `$DAY_DIR` elsewhere runs stw in the parent of that folder), fixed environment: `PATH`, `HOME`,
  `USER`, `LOGNAME`, `TMPDIR`, `LANG`, `LC_ALL`, `LC_CTYPE`, `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_CACHE_HOME`, `GH_TOKEN`, `GITHUB_TOKEN`,
  `GH_CONFIG_DIR`, `SSH_AUTH_SOCK`, proxy variables, `NO_COLOR=1`. Output is limited to 16 MiB, stderr beyond 64 KiB is dropped.
  `--offline` is passed on; with a file it is a usage error. The Go stw prints schema 1 with `--offline`, so `day briefing --offline` with
  the Go stw reports `Go-stw liefert offline schema 1; ohne --offline aufrufen`. stw exit 1 (a repository cannot be read) with a valid document is
  used; any other non-zero exit is an error.
- Block: it sits between the lines `<!-- stw:begin -->` and `<!-- stw:end -->` under the heading `## stw-Fakten`; every run replaces only
  the lines between the markers, atomically (temp file in the same directory, then rename; a symlinked `DAYBOARD.md` is followed, the file mode
  stays). Without markers the block is appended once. The block does not count toward the section limits (a section ends at the marker).
  Content: `Stand <date time>, Quelle: stw status --json (schema 2)`, one counts line per repository and `All` (as `stw status` prints them:
  `n+` is a lower bound, `?` not readable, `n/a` not read; never 0 for a missing read), then the list of things to do (ten at most, then
  `… und N weitere (stw status)`), the notices (ten at most) and a summary line. Two producers use `schema_version` 2 and day marks which one it
  read: the Go engine has `actions` (with priority) and `notices`: `Handlungspunkte (N)` by priority, `Hinweise (N)`. The Rust stw has neither: `Repos mit
  Aufmerksamkeit (N)` (repositories with an attention reason in JSON order, at most five reasons each) and `Hinweise (abgeleitet, N)` (derived from
  `github_note`, the pull request error per repository and issue reads that failed) until it delivers a `notices` array. Missing fields
  (for example no `counts`, as in the installed Go release) are shown as `keine Zähler im JSON`. The same run is idempotent: only the Stand line changes.
  The board is read again after the stw run and checked again right before the rename, so an entry the coordinator sets meanwhile is kept; a CRLF
  board keeps its line endings.
- Text from the JSON (names, titles, messages) is untrusted: control characters, ESC, CR/LF and bidi characters are written as `\x1b`,
  `\n`, `\u202e`, lengths are bounded, `<` and `>` become entities, and every such text sits in a `- ` list line, so it cannot forge a marker
  or a heading.
- Output: the plan overview (Jetzt, Heute, Wartet auf Owner in short form), `Schuldenbuch: N offen, M älter als 3 Tage`, then the block.
  `--print` writes nothing. `--date` and `--time` set the Stand line (default: now).
- Errors (stw missing, fails, over the limit, unknown `schema_version`, broken JSON, missing required fields `command`, `repos`, `summary`,
  `repos[].name`): the line `stw status nicht verfügbar: <reason>` is printed, the old block stays, its Stand line gets
  `; veraltet seit <time>: <reason>` once, exit 0. Exit 2 only for usage errors, an unreadable input file, an unreadable or unwritable
  dayboard directory and a damaged marker structure (one marker, repeated or swapped markers: reported with line numbers, nothing written), and a board that keeps changing during the write (three attempts, nothing written). A signal to `day` (Ctrl-C, SIGTERM, SIGHUP) kills the stw process group as well, writes nothing and exits with 128 plus the signal number (130, 143, 129). On Windows `day` builds, but stw is not put in its own process group (no group kill); only Linux and macOS are released.
- `check` requires exactly one `<!-- stw:begin -->` before exactly one `<!-- stw:end -->` (findings with line numbers). `rollover` leaves the
  block as it is: it is neither moved to the logbook nor deleted.
- `$DAY_STW_TIMEOUT_SEC` shortens the stw limit (for tests).

## Git model

The dayboard folder is part of the enclosing (workspace) repository, not a repository of its own:

- `dayboard/BACKLOG.md` and `dayboard/LOGBOOK.md` are tracked by the workspace repo. History lives in `LOGBOOK.md`.
- `dayboard/DAYBOARD.md` is ignored there (`.gitignore` line `dayboard/DAYBOARD.md`) and never committed. `init` prints a
  hint when it is not ignored yet. `init` and `rollover` never edit ignore files.
- `init` commits the files it created, `rollover` commits `LOGBOOK.md` when it appended to it, each with
  `git commit --only -- <paths>`: other staged files stay staged and are not committed. Commit identity:
  `DAY_GIT_NAME` and `DAY_GIT_EMAIL` override name and email independently when set; git resolves whatever is unset (repo config before global).
  A failed commit unstages exactly the files `day` staged. Files left untracked by an earlier failed commit are committed by the next `init`.
- Outside any git repository the files are only written, no commit, no error. If the enclosing repo is in the middle of a
  rebase or merge, or the commit fails, `day` warns and leaves the files changed (exit 0 once the board is written).
- Legacy: a folder that still has its own `.git` keeps working as before (commits, including `DAYBOARD.md`, go into
  that nested repo) and prints one migration hint. Migrate by hand: move `dayboard/.git` away, remove `/dayboard/` from `.git/info/exclude` (old `day init` wrote it) and a `dayboard/`
  line from `.gitignore` (old whr-init wrote it), then
  `git add dayboard/BACKLOG.md dayboard/LOGBOOK.md` in the workspace and add `dayboard/DAYBOARD.md` to `.gitignore`.
  There is no automatic migration.

## Tests

    go build -o day . && ./test-day     # shell tests against the binary (DAY=/path/to/day for another build)
    go test ./...                       # unit and command tests (decisions: decisions*_test.go)

The shell tests use throw-away directories under `${TMPDIR:-/tmp}` and a private `$HOME`; they need no real dayboard.
The stw JSON fixtures are in `testdata/stw/` (`real-*.json` are real outputs of the Rust stw, the Go engine, the installed Go release and Go `--offline` (schema 1), paths normalized to `/ws`; the other files are hand-made for failure, attack, truncation and error cases).
This is a prototype tool: there are no mutation proofs.

## Release

Push a tag `v*` to the GitHub repository. `.github/workflows/release.yml` cross-compiles static binaries
(linux and darwin, amd64 and arm64) and attaches `day_<version>_<os>_<arch>.tar.gz` (containing `day`) to the release.
mise's `github:` backend scores release assets by OS and architecture in the name and picks the right one itself.

## Location independence

The folder has no references outside itself and takes data paths by parameter or environment, so it can be split out
with `git subtree split --prefix=day` (the workflow under `.github/` travels with it).
