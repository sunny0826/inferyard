package observe

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

type procStat struct {
	start, cpu uint64
	zombie     bool
}

func parseProcStat(text string) (procStat, error) {
	var p procStat
	end := strings.LastIndex(text, ")")
	if end < 0 {
		return p, ErrUnavailable
	}
	f := strings.Fields(text[end+1:])
	if len(f) < 20 {
		return p, ErrUnavailable
	}
	user, e1 := strconv.ParseUint(f[11], 10, 64)
	system, e2 := strconv.ParseUint(f[12], 10, 64)
	start, e3 := strconv.ParseUint(f[19], 10, 64)
	if e1 != nil || e2 != nil || e3 != nil || start == 0 || math.MaxUint64-user < system {
		return p, ErrUnavailable
	}
	return procStat{start, user + system, f[0] == "Z"}, nil
}

func parseMemValue(text, key string) (uint64, error) {
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) > 0 && f[0] == key+":" {
			if len(f) != 3 || f[2] != "kB" {
				return 0, errors.New("memory_unit_invalid")
			}
			v, err := strconv.ParseUint(f[1], 10, 64)
			if err != nil || v > math.MaxUint64/1024 {
				return 0, errors.New("memory_value_invalid")
			}
			return v * 1024, nil
		}
	}
	return 0, errors.New("memory_field_missing")
}

func ticksNS(ticks, hz uint64) (uint64, error) {
	if hz == 0 || hz > 1_000_000_000 || ticks/hz > math.MaxUint64/1_000_000_000 {
		return 0, ErrUnavailable
	}
	v := ticks / hz * 1_000_000_000
	remainder := ticks % hz * 1_000_000_000 / hz
	if v > math.MaxUint64-remainder {
		return 0, ErrUnavailable
	}
	return v + remainder, nil
}
