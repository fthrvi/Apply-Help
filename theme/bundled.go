package theme

import (
	_ "embed"
	"fyne.io/fyne/v2"
)

//go:embed Lexend-VariableFont_wght.ttf
var resourceLexendVariableFontwghtTtfData []byte
var resourceLexendVariableFontwghtTtf = &fyne.StaticResource{
	StaticName:    "Lexend-VariableFont_wght.ttf",
	StaticContent: resourceLexendVariableFontwghtTtfData,
}

//go:embed Notable-Regular.ttf
var resourceNotableRegularTtfData []byte
var resourceNotableRegularTtf = &fyne.StaticResource{
	StaticName:    "Notable-Regular.ttf",
	StaticContent: resourceNotableRegularTtfData,
}

//go:embed Montserrat-VariableFont_wght.ttf
var resourceMontserratVariableFontwghtTtfData []byte
var resourceMontserratVariableFontwghtTtf = &fyne.StaticResource{
	StaticName:    "Montserrat-VariableFont_wght.ttf",
	StaticContent: resourceMontserratVariableFontwghtTtfData,
}

//go:embed Montserrat-Bold.ttf
var resourceMontserratBoldTtfData []byte
var resourceMontserratBoldTtf = &fyne.StaticResource{
	StaticName:    "Montserrat-Bold.ttf",
	StaticContent: resourceMontserratBoldTtfData,
}
