package main

import (
	"32-Adarsha/services"
	"32-Adarsha/theme"
	"32-Adarsha/ui"

	"fyne.io/fyne/v2/app"
)

func main() {
	myApp := app.New()

	// 1. APPLY THE CUSTOM FONT THEME
	// This ensures Lexend and Notable are used throughout the app
	myApp.Settings().SetTheme(theme.MyCustomTheme{})

	// 2. Initialize Database
	db := services.InitDb()
	defer db.Close()

	// 3. Launch UI
	myWindow := ui.CreateMainWindow(myApp, db)
	// full screen
	myWindow.SetFullScreen(true)
	myWindow.ShowAndRun()
}
