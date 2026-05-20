package theme

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

type MyCustomTheme struct{} // Don't embed the interface here to keep it simple

// Font is the only thing we actually want to change
func (m MyCustomTheme) Font(s fyne.TextStyle) fyne.Resource {
	if s.Bold {
		return resourceMontserratBoldTtf
	}
	return resourceLexendVariableFontwghtTtf
}

// For everything else, explicitly return the DefaultTheme values
func (m MyCustomTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	return theme.DefaultTheme().Color(n, v)
}

func (m MyCustomTheme) Icon(n fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(n)
}

func (m MyCustomTheme) Size(n fyne.ThemeSizeName) float32 {
	if n == theme.SizeNameHeadingText {
		return 34 // Make it big
	}
	if n == theme.SizeNameSubHeadingText {
		return 24
	}
	if n == theme.SizeNameText {
		return 16
	}
	return theme.DefaultTheme().Size(n)
}
