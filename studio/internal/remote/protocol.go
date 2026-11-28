package remote

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
)

type wireFrame struct {
	ID     string          `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *RPCError       `json:"error,omitempty"`
	Event  string          `json:"event,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
}

func readWireLine(reader *bufio.Reader, limit int) ([]byte, error) {
	var line []byte
	for {
		fragment, err := reader.ReadSlice('\n')
		line = append(line, fragment...)
		if len(line) > limit {
			return nil, fmt.Errorf("protocol line exceeds %d bytes", limit)
		}
		if err == nil {
			return line, nil
		}
		if err != bufio.ErrBufferFull {
			return nil, err
		}
	}
}

func decodeWireLine(reader *bufio.Reader) (wireFrame, error) {
	line, err := readWireLine(reader, MaxControlLineBytes)
	if err != nil {
		return wireFrame{}, err
	}
	var frame wireFrame
	if err := json.Unmarshal(line, &frame); err != nil {
		return wireFrame{}, err
	}
	return frame, nil
}

func encodeWireFrame(frame wireFrame) ([]byte, error) {
	data, err := json.Marshal(frame)
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	if len(data) > MaxControlLineBytes {
		return nil, fmt.Errorf("control frame exceeds maximum allowed size")
	}
	return data, nil
}

func writeJSONLine(writer io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > MaxControlLineBytes {
		return fmt.Errorf("control frame exceeds maximum allowed size")
	}
	_, err = writer.Write(data)
	return err
}

func randomID(prefix string) (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(value[:]), nil
}
