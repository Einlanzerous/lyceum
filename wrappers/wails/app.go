package main

import (
	"context"

	rt "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/magos/lyceum/wrappers/wails/winstate"
)

// App is the Wails application backend. The reader is a pure client of the
// remote Lyceum server (all state lives there and syncs over HTTP), so there
// are no bound Go methods — native behaviour is limited to menu callbacks and
// window-state persistence (LYCM-305), which use the Wails runtime directly.
type App struct {
	ctx context.Context

	stateFile string
	saved     winstate.State
	savedOK   bool
}

// NewApp constructs the App, loading the previous session's window geometry.
func NewApp(stateFile string) *App {
	a := &App{stateFile: stateFile}
	a.saved, a.savedOK = winstate.Load(stateFile)
	return a
}

// startup captures the runtime context and restores the saved window position.
// Size and maximised-ness are applied via options in main.go — position has no
// launch option, only this runtime call.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if a.savedOK && a.saved.PosValid && !a.saved.Maximised {
		rt.WindowSetPosition(ctx, a.saved.X, a.saved.Y)
	}
}

// beforeClose persists the window geometry, then lets the close proceed.
func (a *App) beforeClose(ctx context.Context) bool {
	// Closed while minimised the window reports the -32000 sentinel, and
	// maximised/fullscreen geometry isn't the size worth restoring to — in
	// those cases keep the last good geometry and update only the flag.
	if rt.WindowIsMinimised(ctx) {
		return false
	}
	st := a.saved
	st.Maximised = rt.WindowIsMaximised(ctx)
	if !st.Maximised && !rt.WindowIsFullscreen(ctx) {
		st.Width, st.Height = rt.WindowGetSize(ctx)
		st.X, st.Y = rt.WindowGetPosition(ctx)
	} else if !a.savedOK {
		st.Width, st.Height = defaultWidth, defaultHeight
	}
	if err := winstate.Save(a.stateFile, st); err != nil {
		println("lyceum-desktop: window-state save failed:", err.Error())
	}
	return false
}
