package cli

import (
	"bytes"
	"strings"
	"testing"

	"google.golang.org/protobuf/types/known/structpb"

	controlv1 "inferencerig/core/rpc/gen/v1"
)

// Every test here renders into a bytes.Buffer, which is not a terminal, so
// style.Interactive answers false and the output carries no ANSI escapes.
// That is what lets these be plain substring assertions rather than
// escape-sequence archaeology.

func TestRenderProtoTabulatesRepeatedMessages(t *testing.T) {
	var out bytes.Buffer
	response := &controlv1.ListLocalModelsResponse{
		Ok: true,
		Models: []*controlv1.LocalModel{
			{Path: "/models/a.gguf", Filename: "a.gguf", SizeBytes: 6 * 1024 * 1024 * 1024},
			{Path: "/models/b.gguf", Filename: "b.gguf", SizeBytes: 1024},
		},
	}
	if err := renderProto(&out, response); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	// The column header loses the _bytes suffix because the value carries the
	// unit; asserting both together pins the pair that has to stay consistent.
	if !strings.Contains(text, "SIZE") || strings.Contains(text, "SIZE BYTES") {
		t.Errorf("want a SIZE column without the redundant unit, got:\n%s", text)
	}
	if !strings.Contains(text, "6.0 GiB") || !strings.Contains(text, "1.0 KiB") {
		t.Errorf("want byte counts formatted, got:\n%s", text)
	}
	for _, row := range []string{"a.gguf", "b.gguf"} {
		if !strings.Contains(text, row) {
			t.Errorf("row %q missing from:\n%s", row, text)
		}
	}
}

// The top-level ok flag is dropped because the RPC already succeeded; a nested
// one is kept because it can be false.
func TestRenderProtoDropsTopLevelOK(t *testing.T) {
	var out bytes.Buffer
	if err := renderProto(&out, &controlv1.HealthResponse{Ok: true, Service: "control"}); err != nil {
		t.Fatal(err)
	}
	if text := out.String(); strings.Contains(strings.ToLower(text), "ok:") {
		t.Errorf("top-level ok should not be rendered, got:\n%s", text)
	}
}

func TestRenderProtoReportsEmptyListAsNone(t *testing.T) {
	var out bytes.Buffer
	if err := renderProto(&out, &controlv1.ListLocalModelsResponse{Ok: true}); err != nil {
		t.Fatal(err)
	}
	// An empty repeated field is elided by protoreflect entirely, so the
	// message body is empty rather than a bare table header. Either way the
	// one thing that must not happen is a header with no rows under it.
	if text := out.String(); strings.Contains(text, "PATH") {
		t.Errorf("empty list rendered a header with no rows:\n%s", text)
	}
}

// A Struct's values are Value messages. Rendering one without unwrapping
// prints the Go pointer behind it, which is what this pins against.
func TestRenderProtoUnwrapsStructValues(t *testing.T) {
	args, err := structpb.NewStruct(map[string]any{"ngl": "auto", "ctx-size": "65536"})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := renderProto(&out, &controlv1.GetProfileResponse{
		Ok:      true,
		Profile: &controlv1.Profile{Name: "demo", Backend: "llamacpp", EngineArgs: args},
	}); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "ngl") || !strings.Contains(text, "auto") {
		t.Errorf("engine args not rendered as pairs:\n%s", text)
	}
	if strings.Contains(text, "0x") {
		t.Errorf("a struct value leaked its pointer:\n%s", text)
	}
}

func TestTitleize(t *testing.T) {
	for input, want := range map[string]string{
		"running_profiles": "Running profiles",
		"runningProfiles":  "Running profiles",
		"name":             "Name",
		"size_bytes":       "Size",
		"used_percent":     "Used percent",
	} {
		if got := titleize(input); got != want {
			t.Errorf("titleize(%q) = %q, want %q", input, got, want)
		}
	}
}

// joinCells must measure with lipgloss.Width, not len: a coloured cell carries
// escapes that occupy no columns, and padding by len skews the whole table.
func TestJoinCellsPadsByDisplayWidth(t *testing.T) {
	widths := columnWidths([][]string{{"aaa", "b"}, {"a", "bbb"}})
	row := joinCells([]string{"a", "b"}, widths)
	if got := strings.Index(row, "b"); got != 5 {
		t.Errorf("second column starts at %d, want 5 (3-wide column + 2 gap): %q", got, row)
	}
	if strings.HasSuffix(row, " ") {
		t.Errorf("row has trailing whitespace: %q", row)
	}
}
