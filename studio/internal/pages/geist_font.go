package pages

import (
	_ "embed"
	"sync"

	"github.com/egoist/mygo/ui"
)

const geistFontFamily = "Geist"

//go:embed fonts/Geist-Regular.ttf
var geistRegularTTF []byte

//go:embed fonts/Geist-Medium.ttf
var geistMediumTTF []byte

//go:embed fonts/Geist-SemiBold.ttf
var geistSemiBoldTTF []byte

//go:embed fonts/Geist-Bold.ttf
var geistBoldTTF []byte

var geistFontOnce sync.Once

func ensureGeistFont() {
	geistFontOnce.Do(func() {
		for _, data := range [][]byte{
			geistRegularTTF,
			geistMediumTTF,
			geistSemiBoldTTF,
			geistBoldTTF,
		} {
			_ = ui.RegisterFont(data, geistFontFamily)
		}
	})
}
