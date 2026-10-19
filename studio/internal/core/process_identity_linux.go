package core

import (
	"os"
	"strconv"
	"strings"
)

func processStartTicks(pid int64) (int64, error) {
	raw, err := os.ReadFile("/proc/" + strconv.FormatInt(pid, 10) + "/stat")
	if err != nil {
		return 0, err
	}
	return parseProcessStat(raw)
}

func parseProcessStat(raw []byte) (int64, error) {
	end := strings.LastIndexByte(string(raw), ')')
	if end < 0 {
		return 0, os.ErrInvalid
	}
	fields := strings.Fields(string(raw)[end+1:])
	if len(fields) < 20 {
		return 0, os.ErrInvalid
	}
	if fields[0] == "Z" || fields[0] == "X" {
		return 0, os.ErrNotExist
	}
	return strconv.ParseInt(fields[19], 10, 64)
}
