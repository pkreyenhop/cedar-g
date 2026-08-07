package main

import (
	"image/color"
	"os"
	"path/filepath"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// The viewer emulates the monochrome look of the Xerox Cedar / Tioga environment
// (see http://toastytech.com/guis/cedar.html): black text on white "viewers"
// with square 1px-black borders, thin black rule separators, a light-grey chrome
// and a serif body font (Cedar used a font it called "TimesRoman").

var (
	cedarWhite    = color.NRGBA{0xff, 0xff, 0xff, 0xff}
	cedarBlack    = color.NRGBA{0x00, 0x00, 0x00, 0xff}
	cedarGrey     = color.NRGBA{0xd0, 0xd0, 0xd0, 0xff} // light chrome (caption/menus)
	cedarGreyMid  = color.NRGBA{0xa8, 0xa8, 0xa8, 0xff} // selection / hover / tray
	cedarGreyDark = color.NRGBA{0x70, 0x70, 0x70, 0xff} // scrollbar / placeholders
)

// fontSet holds the serif family (and a monospace) loaded from the host system.
// Any member may be nil, in which case the base theme's font is used.
type fontSet struct {
	serif, serifB, serifI, serifBI, mono fyne.Resource
}

var (
	fontsOnce sync.Once
	fonts     fontSet
)

// loadFontResource returns the first readable font file from paths, or nil.
func loadFontResource(paths ...string) fyne.Resource {
	for _, p := range paths {
		if b, err := os.ReadFile(p); err == nil {
			return fyne.NewStaticResource(filepath.Base(p), b)
		}
	}
	return nil
}

// cedarFonts loads the Cedar-like serif family once, trying macOS then common
// Linux locations. Missing fonts simply fall back to Fyne's default.
func cedarFonts() fontSet {
	fontsOnce.Do(func() {
		const mac = "/System/Library/Fonts/Supplemental/"
		// Georgia is a heavier, screen-optimised serif (far more readable than
		// Times at these sizes) while keeping the Cedar/Tioga serif feel.
		fonts = fontSet{
			serif: loadFontResource(mac+"Georgia.ttf", mac+"Times New Roman.ttf",
				"/usr/share/fonts/truetype/dejavu/DejaVuSerif.ttf",
				"/usr/share/fonts/liberation/LiberationSerif-Regular.ttf"),
			serifB: loadFontResource(mac+"Georgia Bold.ttf", mac+"Times New Roman Bold.ttf",
				"/usr/share/fonts/truetype/dejavu/DejaVuSerif-Bold.ttf",
				"/usr/share/fonts/liberation/LiberationSerif-Bold.ttf"),
			serifI: loadFontResource(mac+"Georgia Italic.ttf", mac+"Times New Roman Italic.ttf",
				"/usr/share/fonts/truetype/dejavu/DejaVuSerif-Italic.ttf",
				"/usr/share/fonts/liberation/LiberationSerif-Italic.ttf"),
			serifBI: loadFontResource(mac+"Georgia Bold Italic.ttf", mac+"Times New Roman Bold Italic.ttf"),
			// Monaco/Menlo read better than Courier New for code.
			mono: loadFontResource("/System/Library/Fonts/Monaco.ttf", mac+"Courier New.ttf",
				"/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf"),
		}
	})
	return fonts
}

// cedarTheme implements fyne.Theme with the Cedar monochrome palette. It also
// carries the zoom scale, so the same object drives font-size zoom.
type cedarTheme struct {
	base  fyne.Theme
	fonts fontSet
	scale float32
}

func newCedarTheme(scale float32) *cedarTheme {
	return &cedarTheme{base: theme.DefaultTheme(), fonts: cedarFonts(), scale: scale}
}

func (t *cedarTheme) Color(n fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch n {
	case theme.ColorNameBackground, theme.ColorNameInputBackground,
		theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground:
		return cedarWhite
	case theme.ColorNameForeground, theme.ColorNamePrimary, theme.ColorNameHyperlink,
		theme.ColorNameSeparator, theme.ColorNameInputBorder, theme.ColorNameInnerWindowBorder:
		return cedarBlack
	case theme.ColorNameForegroundOnPrimary:
		return cedarWhite
	case theme.ColorNameButton, theme.ColorNameDisabledButton,
		theme.ColorNameHeaderBackground, theme.ColorNameScrollBarBackground:
		return cedarGrey
	case theme.ColorNameSelection, theme.ColorNameFocus, theme.ColorNameHover:
		return cedarGreyMid
	case theme.ColorNamePressed:
		return cedarGreyDark
	case theme.ColorNameScrollBar, theme.ColorNamePlaceHolder, theme.ColorNameDisabled:
		return cedarGreyDark
	case theme.ColorNameShadow:
		return color.NRGBA{0, 0, 0, 0} // flat: no drop shadows
	}
	return t.base.Color(n, theme.VariantLight)
}

func (t *cedarTheme) Font(s fyne.TextStyle) fyne.Resource {
	pick := func(r fyne.Resource) fyne.Resource {
		if r != nil {
			return r
		}
		return t.base.Font(s)
	}
	if s.Monospace {
		return pick(t.fonts.mono)
	}
	switch {
	case s.Bold && s.Italic:
		return pick(t.fonts.serifBI)
	case s.Bold:
		return pick(t.fonts.serifB)
	case s.Italic:
		return pick(t.fonts.serifI)
	default:
		return pick(t.fonts.serif)
	}
}

func (t *cedarTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return t.base.Icon(n) }

func (t *cedarTheme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	// Square everything off — Cedar has no rounded corners.
	case theme.SizeNameInputRadius, theme.SizeNameSelectionRadius, theme.SizeNameButtonRadius,
		theme.SizeNameCardRadius, theme.SizeNameDialogRadius, theme.SizeNameMenuRadius,
		theme.SizeNamePopupRadius, theme.SizeNameScrollBarRadius, theme.SizeNameInnerWindowRadius,
		theme.SizeNameWindowButtonRadius:
		return 0
	}
	var s float32
	switch n {
	case theme.SizeNameSeparatorThickness, theme.SizeNameInputBorder:
		s = 1
	case theme.SizeNameScrollBar:
		s = 11
	case theme.SizeNameScrollBarSmall:
		s = 4
	case theme.SizeNamePadding:
		s = 3
	case theme.SizeNameInnerPadding:
		s = 4
	case theme.SizeNameText:
		s = 16
	case theme.SizeNameCaptionText:
		s = 12
	case theme.SizeNameSubHeadingText:
		s = 20
	case theme.SizeNameHeadingText:
		s = 26
	case theme.SizeNameLineSpacing:
		s = 5
	default:
		s = t.base.Size(n)
	}
	return s * t.scale
}
