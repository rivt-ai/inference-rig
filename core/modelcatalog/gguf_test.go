package modelcatalog

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// buildGGUF assembles a minimal in-memory GGUF header with the given
// string->uint32 KV pairs, all typed as ggufTypeUint32.
func buildGGUF(t *testing.T, kv map[string]uint32) []byte {
	t.Helper()
	var buf bytes.Buffer
	write := func(v any) {
		if err := binary.Write(&buf, binary.LittleEndian, v); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	writeString := func(s string) {
		write(uint64(len(s)))
		buf.WriteString(s)
	}

	write(uint32(ggufMagic))
	write(uint32(3))       // version
	write(uint64(0))       // tensor count
	write(uint64(len(kv))) // kv count

	for k, v := range kv {
		writeString(k)
		write(uint32(ggufTypeUint32))
		write(v)
	}
	return buf.Bytes()
}

func TestReadArch(t *testing.T) {
	data := buildGGUF(t, map[string]uint32{
		"llama.block_count":             32,
		"llama.attention.head_count_kv": 8,
		"llama.embedding_length":        4096,
		"llama.attention.key_length":    128,
		"llama.attention.value_length":  128,
		"llama.context_length":          8192, // unrelated key; must be skipped, not misread
	})
	path := filepath.Join(t.TempDir(), "model.gguf")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadArch(path)
	if err != nil {
		t.Fatalf("ReadArch: %v", err)
	}
	want := Arch{BlockCount: 32, HeadCountKV: 8, EmbeddingLength: 4096, KeyLength: 128, ValueLength: 128}
	if got != want {
		t.Errorf("ReadArch() = %+v, want %+v", got, want)
	}
}

func TestReadArchWithoutKeyValueLength(t *testing.T) {
	data := buildGGUF(t, map[string]uint32{
		"qwen2.block_count":             24,
		"qwen2.attention.head_count_kv": 4,
		"qwen2.embedding_length":        3072,
	})
	path := filepath.Join(t.TempDir(), "model.gguf")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadArch(path)
	if err != nil {
		t.Fatalf("ReadArch: %v", err)
	}
	want := Arch{BlockCount: 24, HeadCountKV: 4, EmbeddingLength: 3072}
	if got != want {
		t.Errorf("ReadArch() = %+v, want %+v", got, want)
	}
}

func TestReadArchBadMagic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "model.gguf")
	if err := os.WriteFile(path, []byte("not a gguf file at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadArch(path); err == nil {
		t.Fatal("expected error for bad magic, got nil")
	}
}

func TestReadArchTruncated(t *testing.T) {
	full := buildGGUF(t, map[string]uint32{
		"llama.block_count":             32,
		"llama.attention.head_count_kv": 8,
		"llama.embedding_length":        4096,
	})
	path := filepath.Join(t.TempDir(), "model.gguf")
	if err := os.WriteFile(path, full[:len(full)-4], 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadArch(path); err == nil {
		t.Fatal("expected error for truncated file, got nil")
	}
}

func TestReadArchIncompleteMetadata(t *testing.T) {
	data := buildGGUF(t, map[string]uint32{
		"llama.block_count": 32,
	})
	path := filepath.Join(t.TempDir(), "model.gguf")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadArch(path); err == nil {
		t.Fatal("expected error for incomplete architecture metadata, got nil")
	}
}

func TestReadArchMissingFile(t *testing.T) {
	if _, err := ReadArch(filepath.Join(t.TempDir(), "missing.gguf")); err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}
