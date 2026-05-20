package main

import (
	"32-Adarsha/services"
	"32-Adarsha/theme"
	"32-Adarsha/ui"
	pull "32-Adarsha/joppull"

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

	// 2.5 Pull latest jobs in the background or foreground
	// Let's do it in a goroutine so the UI can launch quickly, 
	// but it'll pull and insert as needed
	go pull.PullLatestJobs(db)

	// 3. Launch UI
	myWindow := ui.CreateMainWindow(myApp, db)
	// full screen
	myWindow.SetFullScreen(true)
	myWindow.ShowAndRun()
}
