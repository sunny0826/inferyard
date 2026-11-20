package e2e

import (
	"bytes"
	"encoding/binary"
)

func fakeGGUF() []byte {
	var buf bytes.Buffer
	buf.WriteString("GGUF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(3))
	_ = binary.Write(&buf, binary.LittleEndian, uint64(1))
	kvs := []struct {
		key  string
		kind uint32
		str  string
		num  uint32
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
