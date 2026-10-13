package pages

import "fmt"

func formatBytes(value *int64) string {
	if value == nil {
		return "缺测"
	}
	if *value >= 1073741824 || *value <= -1073741824 {
		return fmt.Sprintf("%.2f GiB（1 GiB = 1,073,741,824 bytes）", float64(*value)/1073741824)
	}
	return fmt.Sprintf("%.2f MiB（1 MiB = 1,048,576 bytes）", float64(*value)/1048576)
}
