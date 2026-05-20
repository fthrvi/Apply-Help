package services

import (
	"os"
	"path/filepath"
)

// AppDir returns the directory where applyhelp keeps its database, generated
// HTML/PDF output, and any other state. Resolution order:
//
//  1. $APPLYHELP_DIR if set (lets users pin the location, e.g. for testing)
//  2. ${UserConfigDir}/applyhelp (e.g. ~/Library/Application Support/applyhelp
//     on macOS, ~/.config/applyhelp on Linux, %AppData%\applyhelp on Windows)
//  3. The current working directory as a last resort
//
// The directory is created with 0700 permissions on first call.
func AppDir() string {
	if v := os.Getenv("APPLYHELP_DIR"); v != "" {
		_ = os.MkdirAll(v, 0700)
		return v
	}
	if cfg, err := os.UserConfigDir(); err == nil {
		d := filepath.Join(cfg, "applyhelp")
		_ = os.MkdirAll(d, 0700)
		return d
	}
	cwd, _ := os.Getwd()
	return cwd
}
