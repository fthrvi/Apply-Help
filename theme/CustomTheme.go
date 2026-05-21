package theme

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// MyCustomTheme implements fyne.Theme with a cohesive palette anchored
// to the default resume template (cream background + ink text + pink
// accent). Sizes are tuned for a data-dense desktop app — slightly
// smaller body text than Fyne's default 16px, tighter padding,
// thinner separators.
type MyCustomTheme struct{}

var (
	// Background is a near-white off-cream — keeps a touch of warmth so
	// the slate accent doesn't feel cold, but reads as neutral. The
	// accent is slate-700 (Tailwind palette) — calm enterprise tone,
	// not visually fatiguing across long sessions. Resume templates
	// supply their own accents independently of this.
	paletteBg         = color.NRGBA{R: 0xF8, G: 0xFA, B: 0xFC, A: 0xFF} // slate-50
	paletteCard       = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF} // white
	paletteInk        = color.NRGBA{R: 0x0F, G: 0x17, B: 0x2A, A: 0xFF} // slate-900
	paletteAccent     = color.NRGBA{R: 0x33, G: 0x41, B: 0x55, A: 0xFF} // slate-700
	paletteAccentSoft = color.NRGBA{R: 0xE2, G: 0xE8, B: 0xF0, A: 0xFF} // slate-200 (hover)
	paletteMid        = color.NRGBA{R: 0x64, G: 0x74, B: 0x8B, A: 0xFF} // slate-500
	paletteRule       = color.NRGBA{R: 0xE2, G: 0xE8, B: 0xF0, A: 0xFF} // slate-200 (separator)
	paletteBorder     = color.NRGBA{R: 0xCB, G: 0xD5, B: 0xE1, A: 0xFF} // slate-300
	paletteSuccess    = color.NRGBA{R: 0x10, G: 0xB9, B: 0x81, A: 0xFF} // emerald-500
	paletteWarning    = color.NRGBA{R: 0xF5, G: 0x9E, B: 0x0B, A: 0xFF} // amber-500
	paletteError      = color.NRGBA{R: 0xEF, G: 0x44, B: 0x44, A: 0xFF} // red-500
	paletteShadow     = color.NRGBA{R: 0, G: 0, B: 0, A: 0x14}          // soft drop
)

func (m MyCustomTheme) Font(s fyne.TextStyle) fyne.Resource {
	if s.Bold {
		return resourceMontserratBoldTtf
	}
	return resourceLexendVariableFontwghtTtf
}

func (m MyCustomTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	switch n {
	case theme.ColorNameBackground:
		return paletteBg
	case theme.ColorNameForeground:
		return paletteInk
	case theme.ColorNameForegroundOnPrimary, theme.ColorNameForegroundOnSuccess,
		theme.ColorNameForegroundOnWarning, theme.ColorNameForegroundOnError:
		// White text on the dark primary (slate-700) + on solid
		// success/warning/error fills, so the filter-chip / Add-Job /
		// confirm-dialog buttons stay legible.
		return color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	case theme.ColorNamePrimary:
		return paletteAccent
	case theme.ColorNameHover:
		return paletteAccentSoft
	case theme.ColorNameFocus, theme.ColorNameSelection:
		return paletteAccent
	case theme.ColorNameSeparator:
		return paletteRule
	case theme.ColorNameInputBackground:
		return paletteCard
	case theme.ColorNameInputBorder:
		return paletteBorder
	case theme.ColorNamePlaceHolder:
		return paletteMid
	case theme.ColorNameDisabled, theme.ColorNameDisabledButton:
		return paletteMid
	case theme.ColorNameSuccess:
		return paletteSuccess
	case theme.ColorNameWarning:
		return paletteWarning
	case theme.ColorNameError:
		return paletteError
	case theme.ColorNameButton, theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground:
		return paletteCard
	case theme.ColorNameShadow:
		return paletteShadow
	case theme.ColorNameHeaderBackground:
		return paletteBg
	}
	return theme.DefaultTheme().Color(n, v)
}

func (m MyCustomTheme) Icon(n fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(n)
}

func (m MyCustomTheme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	case theme.SizeNameText:
		return 14
	case theme.SizeNamePadding:
		return 6
	case theme.SizeNameInnerPadding:
		return 8
	case theme.SizeNameHeadingText:
		return 28
	case theme.SizeNameSubHeadingText:
		return 20
	case theme.SizeNameCaptionText:
		return 12
	case theme.SizeNameSeparatorThickness:
		return 1
	case theme.SizeNameInputBorder:
		return 1
	case theme.SizeNameInputRadius:
		return 6
	case theme.SizeNameSelectionRadius:
		return 6
	case theme.SizeNameInlineIcon:
		return 18
	}
	return theme.DefaultTheme().Size(n)
}
