package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSections(t *testing.T) {
	lines := strings.Split(template("2026-11-04"), "\n")
	s := sections(lines)
	for _, n := range secs {
		if s[n] == nil {
			t.Fatalf("section %q missing", n)
		}
	}
	// the last section ends at the facts marker, not at end of file
	if e := s["Erledigt heute"][1]; strings.TrimSpace(lines[e]) != begin {
		t.Fatalf("Erledigt heute ends at %q", lines[e])
	}
}

func TestHeadRe(t *testing.T) {
	if m := headRe.FindStringSubmatch("# Dayboard (Stand 2026-11-04)"); m == nil || m[1] != "2026-11-04" {
		t.Fatal("valid header not matched")
	}
	if headRe.MatchString("# Dayboard (Stand 2026-11-4)") {
		t.Fatal("invalid header matched")
	}
}

// gitRepo creates a git repo in a fresh temp dir and returns its real path.
func gitRepo(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", d, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	return d
}

func chdir(t *testing.T, d string) {
	t.Helper()
	old, _ := os.Getwd()
	if err := os.Chdir(d); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })
}

func TestResolveDirInitDefaultsToMissingDayboard(t *testing.T) {
	t.Setenv("DAY_DIR", "")
	top := gitRepo(t)
	chdir(t, top)
	if got, want := resolveDir("", true), filepath.Join(top, "dayboard"); got != want {
		t.Fatalf("resolveDir = %q, want %q", got, want)
	}
}

func TestResolveDirGivenAndEnvWin(t *testing.T) {
	top := gitRepo(t)
	chdir(t, top)
	a, b := filepath.Join(top, "a"), filepath.Join(top, "b")
	t.Setenv("DAY_DIR", b)
	if got := resolveDir(a, true); got != a {
		t.Fatalf("--dir should win, got %q", got)
	}
	if got := resolveDir("", true); got != b {
		t.Fatalf("$DAY_DIR should win, got %q", got)
	}
	if got := resolveDir("", false); got != b {
		t.Fatalf("$DAY_DIR should win for rollover/check, got %q", got)
	}
}

// runDay runs the test binary itself as `day` via TestHelperProcess and returns exit code and stderr.
func runDay(t *testing.T, dir string, env []string, args ...string) (int, string) {
	t.Helper()
	c := exec.Command(os.Args[0], append([]string{"-test.run=TestHelperProcess", "--"}, args...)...)
	c.Dir = dir
	c.Env = append(os.Environ(), "DAY_HELPER=1", "DAY_DIR=", "DAY_GIT_NAME=Tester", "DAY_GIT_EMAIL=t@example.invalid")
	c.Env = append(c.Env, env...)
	var stderr strings.Builder
	c.Stderr = &stderr
	err := c.Run()
	rc := 0
	if ee, ok := err.(*exec.ExitError); ok {
		rc = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return rc, stderr.String()
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("DAY_HELPER") != "1" {
		return
	}
	var args []string
	for i, a := range os.Args {
		if a == "--" {
			args = os.Args[i+1:]
		}
	}
	os.Exit(run(args))
}

func TestInitCreatesDefaultDayboard(t *testing.T) {
	top := gitRepo(t)
	home := t.TempDir()
	rc, se := runDay(t, top, []string{"HOME=" + home}, "init")
	if rc != 0 {
		t.Fatalf("init rc=%d stderr=%s", rc, se)
	}
	for _, f := range []string{"DAYBOARD.md", "LOGBOOK.md", "BACKLOG.md"} {
		if _, err := os.Stat(filepath.Join(top, "dayboard", f)); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}
	if _, err := os.Stat(filepath.Join(top, "dayboard", ".git")); err == nil {
		t.Error("dayboard must not get its own .git")
	}
}

func gitOut(t *testing.T, d string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", d, "-c", "user.name=x", "-c", "user.email=x@x"}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestResolveDirFromLinkedWorktree(t *testing.T) {
	t.Setenv("DAY_DIR", "")
	top := gitRepo(t)
	gitOut(t, top, "commit", "-q", "--allow-empty", "-m", "root")
	wt := filepath.Join(top, "wt")
	gitOut(t, top, "worktree", "add", "-q", "-b", "b", wt)
	chdir(t, wt)
	if got, want := resolveDir("", true), filepath.Join(top, "dayboard"); got != want {
		t.Fatalf("resolveDir = %q, want %q", got, want)
	}
}

func TestInitCommitsOnlyBacklogAndLogbook(t *testing.T) {
	top := gitRepo(t)
	gitOut(t, top, "commit", "-q", "--allow-empty", "-m", "root")
	os.WriteFile(filepath.Join(top, "staged.txt"), []byte("s\n"), 0o644)
	gitOut(t, top, "add", "staged.txt")
	if rc, se := runDay(t, top, nil, "init", "--date", "2026-11-04"); rc != 0 {
		t.Fatalf("rc=%d %s", rc, se)
	}
	if got := gitOut(t, top, "show", "--name-only", "--format=", "HEAD"); got != "dayboard/BACKLOG.md\ndayboard/LOGBOOK.md" {
		t.Fatalf("commit files = %q", got)
	}
	if got := gitOut(t, top, "status", "--porcelain"); !strings.Contains(got, "A  staged.txt") || !strings.Contains(got, "?? dayboard/DAYBOARD.md") {
		t.Fatalf("status = %q", got)
	}
}

func TestInitOutsideRepoWithDirJustWrites(t *testing.T) {
	d := t.TempDir()
	rc, se := runDay(t, d, []string{"GIT_CEILING_DIRECTORIES=" + filepath.Dir(d)}, "init", "--dir", filepath.Join(d, "b"))
	if rc != 0 || se != "" {
		t.Fatalf("rc=%d stderr=%s", rc, se)
	}
	if _, err := os.Stat(filepath.Join(d, "b", ".git")); err == nil {
		t.Fatal("no .git expected")
	}
	if _, err := os.Stat(filepath.Join(d, "b", "BACKLOG.md")); err != nil {
		t.Fatal(err)
	}
}

func TestInitOutsideGitErrors(t *testing.T) {
	d := t.TempDir()
	rc, se := runDay(t, d, []string{"GIT_CEILING_DIRECTORIES=" + filepath.Dir(d)}, "init")
	if rc != 2 || !strings.Contains(se, "--dir") {
		t.Fatalf("rc=%d stderr=%s", rc, se)
	}
	if _, err := os.Stat(filepath.Join(d, "dayboard")); err == nil {
		t.Fatal("dayboard must not be created")
	}
}

func TestRolloverWithoutDayboardErrors(t *testing.T) {
	top := gitRepo(t)
	rc, se := runDay(t, top, nil, "rollover", "--date", "2026-11-04")
	if rc != 2 || !strings.Contains(se, "--dir") {
		t.Fatalf("rc=%d stderr=%s", rc, se)
	}
	if _, err := os.Stat(filepath.Join(top, "dayboard")); err == nil {
		t.Fatal("dayboard must not be created")
	}
}
