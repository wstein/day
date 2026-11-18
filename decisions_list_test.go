package main

import (
	"strings"
	"testing"
)

func listIDs(out string) []string {
	var ids []string
	for _, l := range strings.Split(out, "\n") {
		if f := strings.Fields(l); len(f) > 0 && strings.HasPrefix(f[0], "D-") {
			ids = append(ids, f[0])
		}
	}
	return ids
}

func TestDecisionsListTable(t *testing.T) {
	_, dir := decWS(t, decBase)
	rc, out, errOut := runDayOut(t, t.TempDir(), nil, "decisions", "list", "--dir", dir)
	if rc != 0 {
		t.Fatalf("rc %d, out %q, err %q", rc, out, errOut)
	}
	want := []string{"D-20261103-01", "D-20261103-02", "D-20261104-01", "D-20261104-02"}
	if got := listIDs(out); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ids %v, want %v (file order)", got, want)
	}
	first := strings.SplitN(out, "\n", 2)[0]
	for _, h := range []string{"ID", "DATUM", "STATUS", "GELTUNG", "ENTSCHEIDER", "AUSSAGE"} {
		if !strings.Contains(first, h) {
			t.Errorf("header %q lacks %q", first, h)
		}
	}
	if !strings.Contains(out, "Dritte | mit | Pipe.") {
		t.Errorf("statement with pipes cut: %q", out)
	}
}

func TestDecisionsListFilters(t *testing.T) {
	_, dir := decWS(t, decBase)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--status", "decided"}, "D-20261103-01,D-20261104-01"},
		{[]string{"--status", "proposed"}, "D-20261104-02"},
		{[]string{"--status", "superseded"}, "D-20261103-02"},
		{[]string{"--scope", "day"}, "D-20261104-01"},
		{[]string{"--scope", "workspace"}, "D-20261103-01,D-20261103-02"},
		{[]string{"--since", "2026-11-04"}, "D-20261104-01,D-20261104-02"},
		{[]string{"--since", "2026-11-05"}, ""},
		{[]string{"--status", "decided", "--since", "2026-11-04"}, "D-20261104-01"},
		{[]string{"--status", "decided", "--scope", "global"}, ""},
	} {
		rc, out, errOut := runDayOut(t, t.TempDir(), nil, append([]string{"decisions", "list", "--dir", dir}, c.args...)...)
		if rc != 0 {
			t.Errorf("%v: rc %d, err %q", c.args, rc, errOut)
			continue
		}
		if got := strings.Join(listIDs(out), ","); got != c.want {
			t.Errorf("%v: ids %q, want %q", c.args, got, c.want)
		}
	}
}

func TestDecisionsListEmptyResult(t *testing.T) {
	_, dir := decWS(t, decBase)
	_, out, _ := runDayOut(t, t.TempDir(), nil, "decisions", "list", "--dir", dir, "--status", "revoked")
	if !strings.Contains(out, "keine Einträge") {
		t.Errorf("empty table: %q", out)
	}
	_, out, _ = runDayOut(t, t.TempDir(), nil, "decisions", "list", "--dir", dir, "--status", "revoked", "--json")
	if out != "{\n  \"schema_version\": 1,\n  \"decisions\": []\n}\n" {
		t.Errorf("empty json: %q", out)
	}
}

func TestDecisionsOpenIsProposed(t *testing.T) {
	_, dir := decWS(t, decBase)
	rc, out, _ := runDayOut(t, t.TempDir(), nil, "decisions", "open", "--dir", dir)
	if rc != 0 || strings.Join(listIDs(out), ",") != "D-20261104-02" {
		t.Fatalf("open: rc %d, out %q", rc, out)
	}
	rc, out, _ = runDayOut(t, t.TempDir(), nil, "decisions", "open", "--dir", dir, "--scope", "workspace")
	if rc != 0 || len(listIDs(out)) != 0 {
		t.Fatalf("open --scope workspace: rc %d, out %q", rc, out)
	}
	if rc, _, _ := runDayOut(t, t.TempDir(), nil, "decisions", "open", "--dir", dir, "--status", "decided"); rc != 2 {
		t.Errorf("open --status must be rejected, rc %d", rc)
	}
}

// Schema pin: the JSON output of list is a stable interface (schema_version 1).
func TestDecisionsListJSONSchemaPin(t *testing.T) {
	_, dir := decWS(t, decHead+
		"- D-20261103-01 | 2026-11-03 | owner | decided | workspace | - | Erste | mit \"Quote\" <b> & Pipe. | [[Goals]]\n"+
		"- D-20261104-01 | 2026-11-04 | owner | decided | day | D-20261103-01 | Zweite. | chat 2026-11-04\n")
	rc, out, _ := runDayOut(t, t.TempDir(), nil, "decisions", "list", "--dir", dir, "--json")
	want := `{
  "schema_version": 1,
  "decisions": [
    {
      "line": 5,
      "id": "D-20261103-01",
      "date": "2026-11-03",
      "by": "owner",
      "status": "decided",
      "scope": "workspace",
      "replaces": null,
      "text": "Erste | mit \"Quote\" <b> & Pipe.",
      "source": "[[Goals]]"
    },
    {
      "line": 6,
      "id": "D-20261104-01",
      "date": "2026-11-04",
      "by": "owner",
      "status": "decided",
      "scope": "day",
      "replaces": "D-20261103-01",
      "text": "Zweite.",
      "source": "chat 2026-11-04"
    }
  ]
}
`
	if rc != 0 || out != want {
		t.Fatalf("rc %d, json:\n%s\nwant:\n%s", rc, out, want)
	}
}

func TestDecisionsListMasksControlCharacters(t *testing.T) {
	_, dir := decWS(t, strings.Replace(decBase, "Erste.", "Er\x1b[31mste\u202e.", 1))
	_, out, _ := runDayOut(t, t.TempDir(), nil, "decisions", "list", "--dir", dir)
	if strings.ContainsAny(out, "\x1b\u202e") || !strings.Contains(out, `Er\x1b[31mste\u202e.`) {
		t.Errorf("control characters not masked: %q", out)
	}
}

func TestDecisionsListErrors(t *testing.T) {
	_, dir := decWS(t, decBase)
	for _, args := range [][]string{
		{"decisions"},
		{"decisions", "bogus"},
		{"decisions", "list", "--status", "agreed"},
		{"decisions", "list", "--since", "2026-13-01"},
		{"decisions", "list", "--text", "x"},
		{"decisions", "open", "--json", "--bogus"},
	} {
		rc, _, errOut := runDayOut(t, t.TempDir(), nil, append(args, "--dir", dir)...)
		if rc != 2 || errOut == "" {
			t.Errorf("%v: rc %d, err %q, want rc 2 with a message", args, rc, errOut)
		}
	}
	_, dir2 := decWS(t, "")
	rc, _, errOut := runDayOut(t, t.TempDir(), nil, "decisions", "list", "--dir", dir2)
	if rc != 2 || !strings.Contains(errOut, "DECISIONS.md") {
		t.Errorf("missing register: rc %d, err %q", rc, errOut)
	}
}
