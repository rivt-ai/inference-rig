package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/antonikliment/tuikit"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"inferencerig/internal/style"
)

// cellWidth caps how wide a single table cell may grow. Model paths run past
// 100 characters, and one such column pushes every other column off the right
// edge of the terminal; eliding the middle keeps both ends, which is where the
// distinguishing part of a path lives.
const cellWidth = 44

// tableGap is the space between table columns: enough to read as a gap, not so
// much that a five-column table stops fitting an 80-column terminal.
const tableGap = 2

// renderProto writes a proto message as human-readable text.
//
// It is deliberately generic rather than a renderer per response type. The
// control service has ~30 response messages and the schema still moves; a
// switch over all of them is a list that goes stale silently, one field at a
// time, and the staleness only shows up as a field quietly missing from the
// output. Walking protoreflect means a new proto field appears in the CLI the
// moment it is added.
//
// Only populated fields are rendered, which matches protojson's default
// behaviour, so the text and JSON modes show the same set of facts.
func renderProto(w io.Writer, message proto.Message) error {
	r := renderer{paint: style.PainterFor(w)}
	_, err := io.WriteString(w, r.message(message.ProtoReflect(), 0))
	return err
}

type renderer struct{ paint style.Painter }

// message renders every populated field of m, indented by depth levels.
func (r renderer) message(m protoreflect.Message, depth int) string {
	var blocks []string
	fields := m.Descriptor().Fields()
	// Iterate the descriptor rather than m.Range: Range's order is undefined,
	// and output that reorders itself between runs cannot be diffed.
	for i := range fields.Len() {
		field := fields.Get(i)
		if !m.Has(field) || isRedundantOK(field, depth) {
			continue
		}
		if block := r.field(field, m.Get(field), depth); block != "" {
			blocks = append(blocks, block)
		}
	}
	if len(blocks) == 0 {
		return tuikit.Indent(r.paint(style.MutedStyle, "(empty)"), depth) + "\n"
	}
	return strings.Join(blocks, "")
}

// isRedundantOK drops the top-level `ok` flag every response carries. By the
// time output is being rendered the RPC returned without error, so the field
// only ever says true — printing "Ok: ✓" above every command trains the reader
// to skip the first line. It is kept in nested messages, where it can be false
// and therefore means something, and always kept under --output json.
func isRedundantOK(field protoreflect.FieldDescriptor, depth int) bool {
	return depth == 0 && field.Name() == "ok" && field.Kind() == protoreflect.BoolKind
}

func (r renderer) field(field protoreflect.FieldDescriptor, value protoreflect.Value, depth int) string {
	label := titleize(string(field.Name()))
	switch {
	case field.IsMap():
		return r.block(label, r.pairs(field, value.Map(), depth+1), depth)
	case field.IsList() && field.Kind() == protoreflect.MessageKind:
		return r.block(label, r.table(field, value.List(), depth+1), depth)
	case field.IsList():
		return r.line(label, r.scalarList(field, value.List()), depth)
	// A Struct is a map wearing a message's clothes; rendering it as a message
	// would print a pointless "Fields:" heading above the pairs.
	case messageIs(field, structType):
		fields := value.Message().Descriptor().Fields().ByName("fields")
		return r.block(label, r.pairs(fields, value.Message().Get(fields).Map(), depth+1), depth)
	case field.Kind() == protoreflect.MessageKind && !messageIs(field, timestampType):
		return r.block(label, r.message(value.Message(), depth+1), depth)
	}
	text := r.scalar(field, value)
	if strings.Contains(text, "\n") {
		return r.block(label, tuikit.IndentLines(text, depth+1), depth)
	}
	return r.line(label, text, depth)
}

func (r renderer) line(label, value string, depth int) string {
	return tuikit.Indent(r.paint(style.MutedStyle, label+":")+" "+value, depth) + "\n"
}

// block renders a heading followed by its already-rendered body.
//
// The trailing blank line separates two adjacent tables that would otherwise
// read as one, but only top-level blocks get it: nested blocks each adding one
// stacks a run of blank lines at the end of every nested structure.
func (r renderer) block(label, body string, depth int) string {
	if body == "" {
		return r.line(label, r.paint(style.MutedStyle, "none"), depth)
	}
	heading := tuikit.Indent(r.paint(style.MutedStyle, label+":"), depth) + "\n"
	if depth > 0 {
		return heading + body
	}
	return heading + body + "\n"
}

// table renders a repeated message field as aligned columns.
//
// Columns are the element fields that are populated somewhere in the list and
// that fit on a line: nested messages, maps and multi-line strings (a profile
// carries its whole YAML document) are dropped, because one of them turns a
// table into an unreadable smear. Nothing is lost by dropping them — the same
// record printed on its own, or under --output json, still has every field.
func (r renderer) table(field protoreflect.FieldDescriptor, list protoreflect.List, depth int) string {
	if list.Len() == 0 {
		return ""
	}
	columns := tableColumns(field.Message(), list)
	if len(columns) == 0 {
		return r.records(list, depth)
	}
	header := make([]string, len(columns))
	for i, column := range columns {
		header[i] = titleize(string(column.Name()))
	}
	rows := make([][]string, 0, list.Len())
	for i := range list.Len() {
		element := list.Get(i).Message()
		row := make([]string, len(columns))
		for j, column := range columns {
			if element.Has(column) {
				row[j] = tuikit.TruncMiddle(r.scalar(column, element.Get(column)), cellWidth)
			}
		}
		rows = append(rows, row)
	}
	return tuikit.IndentLines(style.Theme.Table(r.paint, header, rows, tableGap), depth)
}

// records is the fallback for a repeated message with no line-safe fields:
// print each element as its own indented block rather than an empty table.
func (r renderer) records(list protoreflect.List, depth int) string {
	var b strings.Builder
	for i := range list.Len() {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(r.message(list.Get(i).Message(), depth))
	}
	return b.String()
}

func (r renderer) pairs(field protoreflect.FieldDescriptor, m protoreflect.Map, depth int) string {
	keys := make([]string, 0, m.Len())
	values := map[string]string{}
	m.Range(func(k protoreflect.MapKey, v protoreflect.Value) bool {
		key := k.String()
		keys = append(keys, key)
		values[key] = r.scalar(field.MapValue(), v)
		return true
	})
	if len(keys) == 0 {
		return ""
	}
	slices.Sort(keys)
	ordered := make([]string, len(keys))
	for i, key := range keys {
		ordered[i] = values[key]
	}
	return tuikit.IndentLines(style.Theme.Pairs(r.paint, keys, ordered, tableGap), depth)
}

func (r renderer) scalarList(field protoreflect.FieldDescriptor, list protoreflect.List) string {
	parts := make([]string, list.Len())
	for i := range list.Len() {
		parts[i] = r.scalar(field, list.Get(i))
	}
	return strings.Join(parts, ", ")
}

// scalar formats one value, colouring it by what the field name says it means.
//
// A field name is the only signal available here: protoreflect reports an
// int64, not "a byte count". The heuristic is suffix-based and every branch
// falls through to the raw value, so an unrecognised field renders as it
// always would rather than as something wrong.
func (r renderer) scalar(field protoreflect.FieldDescriptor, value protoreflect.Value) string {
	if text, ok := r.byType(field, value); ok {
		return text
	}
	return r.byName(string(field.Name()), value.String())
}

// byType handles the values whose rendering the schema alone decides. It
// reports false when nothing applied, so the caller can fall through to the
// name-based rules rather than this returning an empty string that a caller
// might mistake for a real value.
func (r renderer) byType(field protoreflect.FieldDescriptor, value protoreflect.Value) (string, bool) {
	name := string(field.Name())
	switch {
	case messageIs(field, timestampType):
		return relative(value.Message().Interface().(*timestamppb.Timestamp)), true
	// A Struct's map values are Value messages. Without unwrapping them, every
	// engine argument renders as the Go pointer behind the message.
	case messageIs(field, valueType):
		return r.structValue(value.Message().Interface().(*structpb.Value)), true
	case field.Kind() == protoreflect.BoolKind:
		if value.Bool() {
			return r.paint(style.SuccessStyle, "✓"), true
		}
		return r.paint(style.MutedStyle, "✗"), true
	case strings.HasSuffix(name, "_bytes"):
		if size, ok := byteCount(field, value); ok {
			return tuikit.FormatBytes(size), true
		}
	// Any *_percent field, not just "percent": raw float64s print sixteen
	// significant digits, and 79.94418931823657 is a worse answer than 79.9%.
	case strings.Contains(name, "percent") && field.Kind() == protoreflect.DoubleKind:
		return fmt.Sprintf("%.1f%%", value.Float()), true
	case field.Kind() == protoreflect.EnumKind:
		return r.status(string(field.Enum().Values().ByNumber(value.Enum()).Name())), true
	}
	return "", false
}

// byName handles the values only the field's name can classify: protoreflect
// reports a string, not "an RFC 3339 instant". Every branch falls through to
// the raw text, so an unrecognised field renders as it always would rather
// than as something wrong.
func (r renderer) byName(name, text string) string {
	switch {
	// Several fields carry an RFC 3339 instant as a plain string rather than
	// a Timestamp; they deserve the same "how long ago" treatment, and parsing
	// is the only way to tell one from an ordinary string.
	case isInstantName(name):
		if at, err := time.Parse(time.RFC3339, text); err == nil {
			return tuikit.Age(at, time.Now())
		}
	// Nanosecond precision on a wall-clock duration is noise in a column an
	// operator is scanning for the one slow entry: 272.914939ms and 272ms lead
	// to the same conclusion, and only one of them lines up.
	case strings.Contains(name, "duration"):
		if d, err := time.ParseDuration(text); err == nil {
			return tuikit.CoarseDuration(d).String()
		}
	case name == "error" && text != "":
		return r.paint(style.ErrorStyle, text)
	case name == "state" || name == "status":
		return r.status(text)
	}
	return text
}

func (r renderer) status(text string) string {
	return style.Theme.StatusWord(r.paint, text)
}

// tableColumns picks the element fields worth a column: populated in at least
// one row, scalar, and single-line.
func tableColumns(descriptor protoreflect.MessageDescriptor, list protoreflect.List) []protoreflect.FieldDescriptor {
	fields := descriptor.Fields()
	var columns []protoreflect.FieldDescriptor
	for i := range fields.Len() {
		field := fields.Get(i)
		if field.IsMap() || field.IsList() {
			continue
		}
		if field.Kind() == protoreflect.MessageKind && !messageIs(field, timestampType) {
			continue
		}
		if usedInAnyRow(field, list) {
			columns = append(columns, field)
		}
	}
	return columns
}

func usedInAnyRow(field protoreflect.FieldDescriptor, list protoreflect.List) bool {
	for i := range list.Len() {
		element := list.Get(i).Message()
		if !element.Has(field) {
			continue
		}
		// A multi-line value (a profile's embedded YAML) cannot share a row
		// with anything, so its presence disqualifies the whole column.
		if strings.Contains(element.Get(field).String(), "\n") {
			return false
		}
		return true
	}
	return false
}

// structValue renders a google.protobuf.Value as the scalar it wraps. Engine
// arguments arrive as a Struct, so this is what makes `ngl: auto` read as
// `ngl: auto` rather than as an address.
func (r renderer) structValue(value *structpb.Value) string {
	switch kind := value.GetKind().(type) {
	case *structpb.Value_StringValue:
		return kind.StringValue
	case *structpb.Value_BoolValue:
		if kind.BoolValue {
			return r.paint(style.SuccessStyle, "✓")
		}
		return r.paint(style.MutedStyle, "✗")
	case *structpb.Value_NullValue:
		return r.paint(style.MutedStyle, "null")
	default:
		// Numbers, nested structs and lists: the JSON form is already the
		// readable one and needs no help from us.
		return value.String()
	}
}

// The well-known types this renderer unwraps rather than descending into.
const (
	timestampType protoreflect.FullName = "google.protobuf.Timestamp"
	structType    protoreflect.FullName = "google.protobuf.Struct"
	valueType     protoreflect.FullName = "google.protobuf.Value"
)

func messageIs(field protoreflect.FieldDescriptor, name protoreflect.FullName) bool {
	return field.Kind() == protoreflect.MessageKind && field.Message().FullName() == name
}

// byteCount reads an integer field as a size, whatever its signedness.
//
// protoreflect.Value.Int panics on an unsigned field and Uint panics on a
// signed one, and the schema uses both (a local model's size_bytes is signed,
// an accelerator's memory_bytes is not). Asking by kind is the only safe way
// in; anything that is not an integer falls back to its raw rendering.
func byteCount(field protoreflect.FieldDescriptor, value protoreflect.Value) (int64, bool) {
	switch field.Kind() {
	case protoreflect.Int32Kind, protoreflect.Int64Kind,
		protoreflect.Sint32Kind, protoreflect.Sint64Kind,
		protoreflect.Sfixed32Kind, protoreflect.Sfixed64Kind:
		return value.Int(), true
	case protoreflect.Uint32Kind, protoreflect.Uint64Kind,
		protoreflect.Fixed32Kind, protoreflect.Fixed64Kind:
		return int64(value.Uint()), true
	default:
		return 0, false
	}
}

// isInstantName covers the spellings the schema actually uses for an instant
// carried as a string: checked_at, commit_time, and a bare time on an event.
func isInstantName(name string) bool {
	return strings.HasSuffix(name, "_at") || strings.HasSuffix(name, "_time") ||
		name == "time" || name == "timestamp"
}

func relative(ts *timestamppb.Timestamp) string {
	if ts == nil || !ts.IsValid() {
		return ""
	}
	return tuikit.Age(ts.AsTime(), time.Now())
}
