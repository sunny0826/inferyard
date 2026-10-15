package e2e

import (
	"bytes"
	"encoding/binary"
)

// fakeGGUF builds the smallest GGUF file the CLI's metadata reader accepts
// (cli/src/inferyard/platforms/gguf_metadata.py): real magic and version, a
// nonzero tensor count, and just the metadata keys device-check inspects —
// general.name, general.architecture, <arch>.context_length, general.file_type
// and tokenizer.chat_template. Tensor infos are never read by the parser, so
// they are legitimately absent.
func fakeGGUF() []byte {
	var buf bytes.Buffer
	buf.WriteString("GGUF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(3)) // version
	_ = binary.Write(&buf, binary.LittleEndian, uint64(1)) // tensor_count (must be nonzero)
	kvs := []struct {
		key   string
		kind  uint32 // 4 = uint32, 8 = string
		str   string
		num   uint32
	}{
		{"general.name", 8, DisplayName, 0},
		{"general.architecture", 8, "llama", 0},
		{"general.file_type", 4, "", 0},
		{"llama.context_length", 4, "", fakeNCtx},
		{"tokenizer.chat_template", 8, "{{ messages }}", 0},
	}
	_ = binary.Write(&buf, binary.LittleEndian, uint64(len(kvs)))
	for _, kv := range kvs {
		_ = binary.Write(&buf, binary.LittleEndian, uint64(len(kv.key)))
		buf.WriteString(kv.key)
		_ = binary.Write(&buf, binary.LittleEndian, kv.kind)
		if kv.kind == 8 {
			_ = binary.Write(&buf, binary.LittleEndian, uint64(len(kv.str)))
			buf.WriteString(kv.str)
		} else {
			_ = binary.Write(&buf, binary.LittleEndian, kv.num)
		}
	}
	return buf.Bytes()
}
