// Command lyceum-desktop is the Wails (Windows) wrapper for the Lyceum reader
// (LYCM-300). It hosts the *same* TypeScript SPA as the web build inside a
// native WebView2 window; the SPA is built with `npm run build:native` so its
// API calls target the user-configured remote backend (see web/src/api/base.ts)
// rather than a same-origin server. The backend's CORS allowlist
// (internal/api.CORS) includes the Wails asset origin so those calls succeed.
//
// This wrapper ships no backend of its own — it is a thin client. The frontend
// bundle is copied into frontend/dist by the build step and embedded below.
package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:     "Lyceum",
		Width:     1100,
		Height:    760,
		MinWidth:  420,
		MinHeight: 560,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup: app.startup,
		Windows: &windows.Options{
			// Match the reader's charcoal background so window chrome doesn't
			// flash white before the SPA paints.
			WebviewIsTransparent: false,
		},
	})
	if err != nil {
		println("lyceum-desktop: fatal:", err.Error())
	}
}
