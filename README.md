# day

Small dayboard tool: `init`, `rollover` and `check` for a dayboard folder (`DAYBOARD.md`, `LOGBOOK.md`, `BACKLOG.md`). The folder has no
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
`LOGBOOK.md` and a `BACKLOG.md` template (existing files are left unchanged; `rollover` and `check` do not touch
`BACKLOG.md`); `rollover` and `check` need an existing board.

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
    go test ./...                       # a few unit tests

The shell tests use throw-away directories under `${TMPDIR:-/tmp}` and a private `$HOME`; they need no real dayboard.
This is a prototype tool: there are no mutation proofs.

## Release

Push a tag `v*` to the GitHub repository. `.github/workflows/release.yml` cross-compiles static binaries
(linux and darwin, amd64 and arm64) and attaches `day_<version>_<os>_<arch>.tar.gz` (containing `day`) to the release.
mise's `github:` backend scores release assets by OS and architecture in the name and picks the right one itself.

## Location independence

The folder has no references outside itself and takes data paths by parameter or environment, so it can be split out
with `git subtree split --prefix=day` (the workflow under `.github/` travels with it).
