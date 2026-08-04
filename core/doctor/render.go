package doctor

import (
	"fmt"
	"io"
	"strings"

	"charm.land/lipgloss/v2"

	"inferencerig/internal/style"
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

// statusStyle maps a check status to its colour. The label text already says
// which is which; the colour only makes the failures findable by eye in a long
// report, so a plain terminal loses speed, not information.
func statusStyle(status Status) lipgloss.Style {
	switch status {
	case StatusFail:
		return style.ErrorStyle
	case StatusWarn:
		return style.WarningStyle
	case StatusSkipped:
		return style.MutedStyle
	default:
		return style.SuccessStyle
	}
}

// WriteText renders the report for a terminal. It shares its data with the
// JSON encoding rather than reformatting it, so the two cannot disagree.
func (r Report) WriteText(w io.Writer) error {
	paint := style.PainterFor(w)
	var b strings.Builder
	fmt.Fprintf(&b, "InferenceRig %s\n%s\n\n", r.Home, r.ConfigPath)
	for _, c := range r.Checks {
		writeCheck(&b, c, paint)
	}
	writeSummary(&b, r)
	_, err := io.WriteString(w, b.String())
	return err
}

func writeCheck(b *strings.Builder, c Check, paint style.Painter) {
	// The label is padded to a fixed width before it is painted: colouring
	// first would put escape sequences inside the field %-22s measures, and
	// every line after the first failure would sit 9 columns to the right.
	fmt.Fprintf(b, "[%s] %-22s %s\n", paint(statusStyle(c.Status), label(c.Status)), c.Title, c.Summary)
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
