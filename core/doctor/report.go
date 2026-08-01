package doctor

import "time"

// Status is a check's verdict.
//
// Skipped is not a soft failure. A diagnostic exists to be run when the daemon
// is down, so "could not be determined without a running daemon" has to be
// distinguishable from "determined, and wrong" — otherwise every run against a
// stopped daemon reads as a pile of problems.
type Status string

const (
	StatusOK      Status = "ok"
	StatusWarn    Status = "warn"
	StatusFail    Status = "fail"
	StatusSkipped Status = "skip"
)

// SchemaVersion identifies the JSON shape for anything parsing --json output.
const SchemaVersion = 1

// Remedy is a named way out of a failure. ConfigEdit is the literal change an
// operator can apply by hand, so the report stays useful with no daemon, no
// network, and no further commands.
type Remedy struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	ConfigEdit string `json:"config_edit,omitempty"`
	Command    string `json:"command,omitempty"`
}

// Check is one verdict. Both the human and JSON renderings come from this, so
// they cannot drift apart.
type Check struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Status   Status   `json:"status"`
	Summary  string   `json:"summary"`
	Detail   string   `json:"detail,omitempty"`
	Remedies []Remedy `json:"remedies,omitempty"`
}

// Report is a full run.
type Report struct {
	SchemaVersion int            `json:"schema_version"`
	GeneratedAt   time.Time      `json:"generated_at"`
	Home          string         `json:"home"`
	ConfigPath    string         `json:"config_path"`
	Checks        []Check        `json:"checks"`
	Counts        map[Status]int `json:"counts"`
}

// Worst is the most severe status in the report. Skipped never outranks ok:
// an undetermined check is not evidence of a problem.
func (r Report) Worst() Status {
	switch {
	case r.Counts[StatusFail] > 0:
		return StatusFail
	case r.Counts[StatusWarn] > 0:
		return StatusWarn
	default:
		return StatusOK
	}
}

func ok(id, title, summary string) Check {
	return Check{ID: id, Title: title, Status: StatusOK, Summary: summary}
}

func warn(id, title, summary string) Check {
	return Check{ID: id, Title: title, Status: StatusWarn, Summary: summary}
}

func fail(id, title, summary string) Check {
	return Check{ID: id, Title: title, Status: StatusFail, Summary: summary}
}

func skip(id, title, reason string) Check {
	return Check{ID: id, Title: title, Status: StatusSkipped, Summary: reason}
}

func (c Check) withDetail(detail string) Check {
	c.Detail = detail
	return c
}

func (c Check) withRemedies(remedies ...Remedy) Check {
	c.Remedies = remedies
	return c
}
