package modelcatalog

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"
)

// Arch is the subset of a GGUF file's architecture metadata that KV-cache
// sizing needs. Fields are zero when the file did not declare them.
//
// ponytail: this parser reads only the KV table, only the keys sizing needs,
// and stops before the tensor table. Extend it (or add a sibling reader) when
// a consumer needs more than sizing — quant display, arch detection, etc.
type Arch struct {
	BlockCount      uint32
	HeadCountKV     uint32
	EmbeddingLength uint32
	KeyLength       uint32
	ValueLength     uint32
}

const (
	ggufMagic = 0x46554747 // "GGUF" little-endian

	ggufTypeUint8   = 0
	ggufTypeInt8    = 1
	ggufTypeUint16  = 2
	ggufTypeInt16   = 3
	ggufTypeUint32  = 4
	ggufTypeInt32   = 5
	ggufTypeFloat32 = 6
	ggufTypeBool    = 7
	ggufTypeString  = 8
	ggufTypeArray   = 9
	ggufTypeUint64  = 10
	ggufTypeInt64   = 11
	ggufTypeFloat64 = 12
)

// ReadArch reads the GGUF header at path and extracts the architecture fields
// KV-cache sizing needs. It walks the KV table only and stops before the
// tensor table.
func ReadArch(path string) (Arch, error) {
	f, err := os.Open(path)
	if err != nil {
		return Arch{}, err
	}
	defer f.Close()
	return readArch(bufio.NewReader(f))
}

func readArch(r io.Reader) (Arch, error) {
	var magic, version uint32
	if err := binary.Read(r, binary.LittleEndian, &magic); err != nil {
		return Arch{}, fmt.Errorf("gguf: reading magic: %w", err)
	}
	if magic != ggufMagic {
		return Arch{}, fmt.Errorf("gguf: not a GGUF file (magic %#x)", magic)
	}
	if err := binary.Read(r, binary.LittleEndian, &version); err != nil {
		return Arch{}, fmt.Errorf("gguf: reading version: %w", err)
	}
	// Versions 2+ use uint64 counts; version 1 used uint32 but is long
	// obsolete and unsupported here.
	if _, err := readUint64(r); err != nil { // tensor count; unused, header stops before the tensor table
		return Arch{}, fmt.Errorf("gguf: reading tensor count: %w", err)
	}
	kvCount64, err := readUint64(r)
	if err != nil {
		return Arch{}, fmt.Errorf("gguf: reading kv count: %w", err)
	}
	kvCount := uint32(kvCount64)

	var a Arch
	for i := uint32(0); i < kvCount; i++ {
		key, err := readGGUFString(r)
		if err != nil {
			return Arch{}, fmt.Errorf("gguf: reading kv %d key: %w", i, err)
		}
		typ, err := readUint32(r)
		if err != nil {
			return Arch{}, fmt.Errorf("gguf: reading kv %d type: %w", i, err)
		}
		switch {
		case strings.HasSuffix(key, ".block_count"):
			v, err := readUintValue(r, typ)
			if err != nil {
				return Arch{}, err
			}
			a.BlockCount = uint32(v)
		case strings.HasSuffix(key, ".attention.head_count_kv"):
			v, err := readUintValue(r, typ)
			if err != nil {
				return Arch{}, err
			}
			a.HeadCountKV = uint32(v)
		case strings.HasSuffix(key, ".embedding_length"):
			v, err := readUintValue(r, typ)
			if err != nil {
				return Arch{}, err
			}
			a.EmbeddingLength = uint32(v)
		case strings.HasSuffix(key, ".attention.key_length"):
			v, err := readUintValue(r, typ)
			if err != nil {
				return Arch{}, err
			}
			a.KeyLength = uint32(v)
		case strings.HasSuffix(key, ".attention.value_length"):
			v, err := readUintValue(r, typ)
			if err != nil {
				return Arch{}, err
			}
			a.ValueLength = uint32(v)
		default:
			if err := skipGGUFValue(r, typ); err != nil {
				return Arch{}, fmt.Errorf("gguf: skipping kv %q: %w", key, err)
			}
		}
	}
	if a.BlockCount == 0 || a.HeadCountKV == 0 || a.EmbeddingLength == 0 {
		return Arch{}, fmt.Errorf("gguf: incomplete architecture metadata (block_count=%d head_count_kv=%d embedding_length=%d)",
			a.BlockCount, a.HeadCountKV, a.EmbeddingLength)
	}
	return a, nil
}

func readUint32(r io.Reader) (uint32, error) {
	var v uint32
	err := binary.Read(r, binary.LittleEndian, &v)
	return v, err
}

func readUint64(r io.Reader) (uint64, error) {
	var v uint64
	err := binary.Read(r, binary.LittleEndian, &v)
	return v, err
}

// readGGUFString reads a GGUF string: a uint64 length prefix followed by
// that many raw bytes (not NUL-terminated).
func readGGUFString(r io.Reader) (string, error) {
	n, err := readUint64(r)
	if err != nil {
		return "", err
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

// readUintValue reads a scalar GGUF value of the given type and returns it
// widened to uint64. Only integer types are meaningful for the keys this
// parser wants; other types return an error.
func readUintValue(r io.Reader, typ uint32) (uint64, error) {
	switch typ {
	case ggufTypeUint8, ggufTypeInt8, ggufTypeBool:
		var v uint8
		if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
			return 0, err
		}
		return uint64(v), nil
	case ggufTypeUint16, ggufTypeInt16:
		var v uint16
		if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
			return 0, err
		}
		return uint64(v), nil
	case ggufTypeUint32, ggufTypeInt32:
		var v uint32
		if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
			return 0, err
		}
		return uint64(v), nil
	case ggufTypeUint64, ggufTypeInt64:
		return readUint64(r)
	case ggufTypeFloat32:
		var v uint32
		if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
			return 0, err
		}
		return 0, fmt.Errorf("gguf: expected integer value, got float32")
	default:
		return 0, fmt.Errorf("gguf: expected integer value, got type %d", typ)
	}
}

// skipGGUFValue advances r past a single value of the given type without
// materializing it.
func skipGGUFValue(r io.Reader, typ uint32) error {
	switch typ {
	case ggufTypeUint8, ggufTypeInt8, ggufTypeBool:
		_, err := io.CopyN(io.Discard, r, 1)
		return err
	case ggufTypeUint16, ggufTypeInt16:
		_, err := io.CopyN(io.Discard, r, 2)
		return err
	case ggufTypeUint32, ggufTypeInt32, ggufTypeFloat32:
		_, err := io.CopyN(io.Discard, r, 4)
		return err
	case ggufTypeUint64, ggufTypeInt64, ggufTypeFloat64:
		_, err := io.CopyN(io.Discard, r, 8)
		return err
	case ggufTypeString:
		_, err := readGGUFString(r)
		return err
	case ggufTypeArray:
		elemType, err := readUint32(r)
		if err != nil {
			return err
		}
		n, err := readUint64(r)
		if err != nil {
			return err
		}
		for i := uint64(0); i < n; i++ {
			if err := skipGGUFValue(r, elemType); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("gguf: unknown value type %d", typ)
	}
}
