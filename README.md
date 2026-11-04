# day

Small dayboard tool: `init`, rollover and `check` for a dayboard folder (`DAYBOARD.md`, `LOGBOOK.md`, own local git
repository). Python standard library only, no push, no remote.

Files: `day` (bash launcher, resolves its own location, follows symlinks), `day.py` (logic), `test-day` (tests).

## Run

    ./day --help
    DAY_DIR=/path/to/dayboard ./day check

The dayboard data folder is passed with `--dir` or `$DAY_DIR` (default `~/Workspaces/dayboard`). It is not part of this
folder and is not tracked here.

## Tests

    ./test-day                 # tests, then the mutation proofs
    ./test-day --no-mutations  # tests only
    DAY=/path/to/other/day ./test-day

The tests use throw-away directories under `${TMPDIR:-/tmp}` and a private `$HOME`; they need no real dayboard.

## Location independence

The scripts find each other relative to their own location and take data paths by parameter or environment, so this
folder can be split out with `git subtree split --prefix=day`.
