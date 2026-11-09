package pr

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type Status struct {
	Number   int
	Branch   string
	Display  string
	CIMark   string
	URL      string
	IsMerged bool
}

type ghCheck struct {
	Conclusion string `json:"conclusion"`
	Status     string `json:"status"`
	State      string `json:"state"`
}

type ghPR struct {
	Number            int       `json:"number"`
	HeadRefName       string    `json:"headRefName"`
	State             string    `json:"state"`
	IsDraft           bool      `json:"isDraft"`
	ReviewDecision    string    `json:"reviewDecision"`
	StatusCheckRollup []ghCheck `json:"statusCheckRollup"`
	URL               string    `json:"url"`
}

func ParseGHOutput(jsonBytes []byte) (map[string]Status, error) {
	var prs []ghPR
	if err := json.Unmarshal(jsonBytes, &prs); err != nil {
		return nil, fmt.Errorf("unmarshal gh pr list output: %w", err)
	}

	result := make(map[string]Status)
	for _, p := range prs {
		st := Status{
			Number: p.Number,
			Branch: p.HeadRefName,
			URL:    p.URL,
		}

		switch p.State {
		case "MERGED":
			st.Display = "merged"
			st.IsMerged = true
		case "CLOSED":
			st.Display = "closed"
		default:
			if p.IsDraft {
				st.Display = "draft"
			} else {
				switch p.ReviewDecision {
				case "APPROVED":
					st.Display = "approved"
				case "CHANGES_REQUESTED":
					st.Display = "changes requested"
				default:
					st.Display = "open"
				}
			}
		}

		st.CIMark = parseCIMark(p.StatusCheckRollup)
		result[p.HeadRefName] = st
	}

	return result, nil
}

func parseCIMark(checks []ghCheck) string {
	if len(checks) == 0 {
		return ""
	}

	hasFailure := false
	hasPending := false
	allSuccess := true

	for _, c := range checks {
		conc := strings.ToUpper(c.Conclusion)
		state := strings.ToUpper(c.State)
		status := strings.ToUpper(c.Status)

		if conc == "FAILURE" || conc == "TIMED_OUT" || conc == "ACTION_REQUIRED" || state == "FAILURE" {
			hasFailure = true
			allSuccess = false
		} else if status == "IN_PROGRESS" || status == "QUEUED" || status == "PENDING" || (conc == "" && state == "PENDING") {
			hasPending = true
			allSuccess = false
		} else if conc != "SUCCESS" && state != "SUCCESS" {
			allSuccess = false
		}
	}

	if hasFailure {
		return "✗"
	}
	if hasPending {
		return "…"
	}
	if allSuccess {
		return "✓"
	}
	return ""
}

func FetchAll(repoRoot string) (map[string]Status, bool, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, false, nil
	}

	cmd := exec.Command("gh", "pr", "list", "--state", "all",
		"--json", "number,headRefName,state,isDraft,reviewDecision,statusCheckRollup,url",
		"--limit", "100")
	cmd.Dir = repoRoot

	out, err := cmd.Output()
	if err != nil {
		return nil, true, fmt.Errorf("gh pr list: %w", err)
	}

	prs, err := ParseGHOutput(out)
	if err != nil {
		return nil, true, err
	}
	return prs, true, nil
}
