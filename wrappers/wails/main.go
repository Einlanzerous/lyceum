// Command lyceum-desktop is the Wails (Windows) wrapper for the Lyceum reader
// (LYCM-300). It hosts the *same* TypeScript SPA as the web build inside a
// native WebView2 window; the SPA is built with `bun run build:native` so its
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
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	rt "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/magos/lyceum/wrappers/wails/winstate"
)

//go:embed all:frontend/dist
var assets embed.FS

const (
	defaultWidth, defaultHeight = 1100, 760
	repoURL                     = "https://github.com/Einlanzerous/lyceum"
)

func main() {
	statePath := winstate.DefaultPath()
	if statePath == "" {
		println("lyceum-desktop: no user config dir; window geometry won't persist")
	}
	app := NewApp(statePath)

	width, height := defaultWidth, defaultHeight
	startState := options.Normal
	if app.savedOK {
		width, height = app.saved.Width, app.saved.Height
		if app.saved.Maximised {
			startState = options.Maximised
		}
	}

	err := wails.Run(&options.App{
		Title:            "Lyceum",
		Width:            width,
		Height:           height,
		MinWidth:         420,
		MinHeight:        560,
		WindowStartState: startState,
		Menu:             appMenu(app),
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:     app.startup,
		OnBeforeClose: app.beforeClose,
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

// appMenu is the native menu bar (LYCM-305). Callbacks only fire after the
// window exists, so app.ctx is always set by then. These are runtime calls,
// not bound methods — the -skipbindings cross-build stays lossless.
func appMenu(app *App) *menu.Menu {
	m := menu.NewMenu()

	file := m.AddSubmenu("File")
	file.AddText("Exit", keys.CmdOrCtrl("q"), func(*menu.CallbackData) {
		rt.Quit(app.ctx)
	})

	view := m.AddSubmenu("View")
	view.AddText("Reload", keys.CmdOrCtrl("r"), func(*menu.CallbackData) {
		rt.WindowReloadApp(app.ctx)
	})
	view.AddText("Toggle Full Screen", keys.Key("f11"), func(*menu.CallbackData) {
		if rt.WindowIsFullscreen(app.ctx) {
			rt.WindowUnfullscreen(app.ctx)
		} else {
			rt.WindowFullscreen(app.ctx)
		}
	})

	help := m.AddSubmenu("Help")
	help.AddText("Lyceum on GitHub", nil, func(*menu.CallbackData) {
		rt.BrowserOpenURL(app.ctx, repoURL)
	})
	help.AddText("Releases", nil, func(*menu.CallbackData) {
		rt.BrowserOpenURL(app.ctx, repoURL+"/releases")
	})

	return m
}
