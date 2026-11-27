// Package reicon provides embedded Reicon (https://reicon.dev) Outline icons for Studio.
// Upstream icons are MIT-licensed from github.com/dqev/reicon.
package reicon

import (
	"embed"
	"sync"

	"github.com/egoist/mygo/ui"
)

//go:embed svg/*.svg
var svgFS embed.FS

var (
	cacheMu sync.Mutex
	cache   = map[string]*ui.SVG{}
)

func load(name string) *ui.SVG {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if svg, ok := cache[name]; ok {
		return svg
	}
	data, err := svgFS.ReadFile("svg/" + name + ".svg")
	if err != nil {
		panic("reicon: missing embedded svg " + name + ": " + err.Error())
	}
	svg := ui.MustParseSVG(data)
	cache[name] = svg
	return svg
}

func Activity() *ui.SVG          { return load("activity") }
func ArrowLeft() *ui.SVG         { return load("arrow-left") }
func ArrowRight() *ui.SVG        { return load("arrow-right") }
func ArrowUp() *ui.SVG           { return load("arrow-up") }
func ArrowUpRight() *ui.SVG      { return load("arrow-up-right") }
func Bell() *ui.SVG              { return load("bell") }
func Box() *ui.SVG               { return load("box") }
func Check() *ui.SVG             { return load("check") }
func ChevronDown() *ui.SVG       { return load("chevron-down") }
func ChevronRight() *ui.SVG      { return load("chevron-right") }
func ChevronUp() *ui.SVG         { return load("chevron-up") }
func CircleAlert() *ui.SVG       { return load("alert-circle") }
func CircleArrowUp() *ui.SVG     { return load("circle-arrow-up") }
func CircleCheck() *ui.SVG       { return load("check-circle") }
func Clock() *ui.SVG             { return load("clock-circle") }
func Code() *ui.SVG              { return load("code") }
func Copy() *ui.SVG              { return load("copy") }
func Cpu() *ui.SVG               { return load("cpu") }
func Download() *ui.SVG          { return load("download") }
func ExternalLink() *ui.SVG      { return load("arrow-up-right-square") }
func Eye() *ui.SVG               { return load("eye") }
func FileSearch() *ui.SVG        { return load("search") }
func FileText() *ui.SVG          { return load("file-text") }
func Folder() *ui.SVG            { return load("folder") }
func FolderOpen() *ui.SVG        { return load("folder-open") }
func FolderPlus() *ui.SVG        { return load("folder-plus") }
func Gauge() *ui.SVG             { return load("gauge") }
func Globe() *ui.SVG             { return load("globe") }
func HardDrive() *ui.SVG         { return load("hard-drive") }
func Inbox() *ui.SVG             { return load("inbox") }
func Info() *ui.SVG              { return load("info-circle") }
func Layers() *ui.SVG            { return load("layers") }
func Link() *ui.SVG              { return load("link") }
func List() *ui.SVG              { return load("list") }
func Monitor() *ui.SVG           { return load("monitor") }
func Moon() *ui.SVG              { return load("moon") }
func PanelBottomClose() *ui.SVG  { return load("minimize") }
func Play() *ui.SVG              { return load("play") }
func Plus() *ui.SVG              { return load("plus") }
func RefreshCw() *ui.SVG         { return load("refresh") }
func RotateCw() *ui.SVG          { return load("rotate-right") }
func Server() *ui.SVG            { return load("server") }
func Settings() *ui.SVG          { return load("settings") }
func SlidersHorizontal() *ui.SVG { return load("sliders") }
func Square() *ui.SVG            { return load("stop") }
func Star() *ui.SVG              { return load("star") }
func Sun() *ui.SVG               { return load("sun") }
func Trash() *ui.SVG             { return load("trash") }
func TriangleAlert() *ui.SVG     { return load("alert-triangle") }
func Unlink() *ui.SVG            { return load("link-off") }
func X() *ui.SVG                 { return load("x") }
