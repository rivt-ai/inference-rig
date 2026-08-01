package doctor

import (
	"fmt"
	"io"
	"strings"
)

// label is the fixed-width status column. Padding is uniform so the statuses
// line up and a failure is findable by shape rather than by reading every line.
func label(status Status) string {
	switch status {
	case StatusFail:
		return "FAIL"
	case StatusWarn:
		return "WARN"
	case StatusSkipped:
		return "SKIP"
	default:
		return "OK  "
	}
}

// WriteText renders the report for a terminal. It shares its data with the
// JSON encoding rather than reformatting it, so the two cannot disagree.
func (r Report) WriteText(w io.Writer) error {
	var b strings.Builder
	fmt.Fprintf(&b, "InferenceRig %s\n%s\n\n", r.Home, r.ConfigPath)
	for _, c := range r.Checks {
		writeCheck(&b, c)
	}
	writeSummary(&b, r)
	_, err := io.WriteString(w, b.String())
	return err
}

func writeCheck(b *strings.Builder, c Check) {
	fmt.Fprintf(b, "[%s] %-22s %s\n", label(c.Status), c.Title, c.Summary)
	for _, line := range nonEmptyLines(c.Detail) {
		fmt.Fprintf(b, "         %s\n", line)
	}
	for _, remedy := range c.Remedies {
		fmt.Fprintf(b, "         → %s\n", remedy.Title)
		for _, line := range nonEmptyLines(remedy.ConfigEdit) {
			fmt.Fprintf(b, "             %s\n", line)
		}
		if remedy.Command != "" {
			fmt.Fprintf(b, "             $ %s\n", remedy.Command)
		}
	}
}

func writeSummary(b *strings.Builder, r Report) {
	parts := []string{}
	for _, status := range []Status{StatusFail, StatusWarn, StatusSkipped} {
		if n := r.Counts[status]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, strings.TrimSpace(label(status))))
		}
	}
	if len(parts) == 0 {
		fmt.Fprintf(b, "\nAll %d checks passed.\n", r.Counts[StatusOK])
		return
	}
	fmt.Fprintf(b, "\n%d checks: %s\n", len(r.Checks), strings.Join(parts, ", "))
}

func nonEmptyLines(text string) []string {
	var kept []string
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			kept = append(kept, line)
		}
	}
	return kept
}
