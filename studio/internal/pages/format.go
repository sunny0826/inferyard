package pages

import "fmt"

func formatBytes(value *int64) string {
	if value == nil {
		return "缺测"
	}
	v := *value
	switch {
	case v >= 1073741824 || v <= -1073741824:
		return fmt.Sprintf("%.2f GiB（%d bytes）", float64(v)/1073741824, v)
	case v >= 1048576 || v <= -1048576:
		return fmt.Sprintf("%.2f MiB（%d bytes）", float64(v)/1048576, v)
	default:
		return fmt.Sprintf("%d bytes", v)
	}
}
