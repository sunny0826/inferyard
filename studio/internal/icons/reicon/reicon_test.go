package reicon

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestAllIconsParse(t *testing.T) {
	icons := map[string]func() *ui.SVG{
		"Activity":          Activity,
		"ArrowLeft":         ArrowLeft,
		"ArrowRight":        ArrowRight,
		"ArrowUp":           ArrowUp,
		"ArrowUpRight":      ArrowUpRight,
		"Bell":              Bell,
		"Box":               Box,
		"Check":             Check,
		"ChevronDown":       ChevronDown,
		"ChevronRight":      ChevronRight,
		"ChevronUp":         ChevronUp,
		"CircleAlert":       CircleAlert,
		"CircleArrowUp":     CircleArrowUp,
		"CircleCheck":       CircleCheck,
		"Clock":             Clock,
		"Code":              Code,
		"Copy":              Copy,
		"Cpu":               Cpu,
		"Download":          Download,
		"ExternalLink":      ExternalLink,
		"Eye":               Eye,
		"FileSearch":        FileSearch,
		"FileText":          FileText,
		"Folder":            Folder,
		"FolderOpen":        FolderOpen,
		"FolderPlus":        FolderPlus,
		"Gauge":             Gauge,
		"Globe":             Globe,
		"HardDrive":         HardDrive,
		"History":           History,
		"Inbox":             Inbox,
		"Info":              Info,
		"Layers":            Layers,
		"Link":              Link,
		"List":              List,
		"Monitor":           Monitor,
		"Moon":              Moon,
		"PanelBottomClose":  PanelBottomClose,
		"Play":              Play,
		"Plus":              Plus,
		"RefreshCw":         RefreshCw,
		"RotateCw":          RotateCw,
		"Server":            Server,
		"Settings":          Settings,
		"SlidersHorizontal": SlidersHorizontal,
		"Square":            Square,
		"Star":              Star,
		"Sun":               Sun,
		"Trash":             Trash,
		"TriangleAlert":     TriangleAlert,
		"Unlink":            Unlink,
		"X":                 X,
	}
	for name, fn := range icons {
		svg := fn()
		if svg == nil {
			t.Fatalf("reicon.%s returned nil", name)
		}
	}
}
