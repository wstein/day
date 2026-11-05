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
	for _, f := range []string{"DAYBOARD.md", "LOGBOOK.md", "BACKLOG.md", ".git"} {
		if _, err := os.Stat(filepath.Join(top, "dayboard", f)); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}
	ex, err := os.ReadFile(filepath.Join(top, ".git", "info", "exclude"))
	if err != nil || !strings.Contains("\n"+string(ex), "\n/dayboard/\n") {
		t.Fatalf("exclude entry missing: %q %v", ex, err)
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
