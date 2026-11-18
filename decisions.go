// decisions: the decision register <dayboard>/DECISIONS.md (check, list, from-chartroom, add).
package main

const decisionsFile = "DECISIONS.md"

// decRow is one register line; Line is the 1-based line number in the file.
type decRow struct {
	Line                                                int
	ID, Date, By, Status, Scope, Replaces, Text, Source string
}

// decStats is the display-only summary shown by check.
type decStats struct {
	Rows, Proposed int
	Stale          []string // proposed rows older than decisionsStaleDays days
}

const decisionsStaleDays = 7

// checkDecisionsText validates the register text: the rows found and the findings (with line numbers).
func checkDecisionsText(text, workspace, chartroom, today string) (rows []decRow, findings []string) {
	return nil, nil
}

// checkDecisions validates dir/DECISIONS.md (a missing file is fine).
func checkDecisions(dir, chartroom, today string) (findings []string, st decStats) {
	return nil, decStats{}
}
