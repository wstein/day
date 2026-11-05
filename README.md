# day

Small dayboard tool: `init`, `rollover` and `check` for a dayboard folder (`DAYBOARD.md`, `LOGBOOK.md`, own local git
repository). One static Go binary, standard library only, no Python, no push, no remote. Needs `git` at run time.

## Install

With mise (release binaries of the GitHub repository `wstein/day`):

    mise use -g github:wstein/day

In the whr dev container `whr-up` installs it like the other tools; a failed install only warns.

## Build

    go build -o day .          # CGO_ENABLED=0 for a static binary; the binary is git-ignored

## Run

    ./day --help
    DAY_DIR=/path/to/dayboard ./day check

The dayboard data folder is passed with `--dir` or `$DAY_DIR` (default: `./dayboard` in the git top level of the
current directory; otherwise an error, exit 2). In a workspace it is `./dayboard`, listed in the workspace `.gitignore`.
It is not part of this folder and is not tracked here.

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
