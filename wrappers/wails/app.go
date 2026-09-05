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
// launch option, only a runtime call.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if !a.savedOK || !a.saved.PosValid || a.saved.Maximised {
		return
	}
	if offscreen(ctx, a.saved.X, a.saved.Y) {
		return
	}
	// WindowGetPosition reports absolute virtual-screen coordinates, but
	// WindowSetPosition treats its arguments as offsets from the current
	// monitor's work-area origin (winc SetPos adds workRect.Left/Top; Pos
	// doesn't subtract it). Restoring the saved absolute position directly
	// therefore drifts by that origin on every launch whenever the work area
	// doesn't start at (0,0) — e.g. a left- or top-docked taskbar. Set, read
	// back, and correct by the observed delta so the position round-trips.
	rt.WindowSetPosition(ctx, a.saved.X, a.saved.Y)
	gx, gy := rt.WindowGetPosition(ctx)
	if dx, dy := gx-a.saved.X, gy-a.saved.Y; dx != 0 || dy != 0 {
		rt.WindowSetPosition(ctx, a.saved.X-dx, a.saved.Y-dy)
	}
}

// offscreen reports whether (x, y) cannot lie on any attached screen. Wails
// exposes screen sizes but not origins, so this bounds the check by the widest
// possible virtual-desktop extent: it catches a position saved on a
// since-detached monitor in the common laid-out-right/below arrangements
// without ever rejecting a position that is still reachable.
func offscreen(ctx context.Context, x, y int) bool {
	screens, err := rt.ScreenGetAll(ctx)
	if err != nil || len(screens) == 0 {
		return false
	}
	var totalW, totalH int
	for _, s := range screens {
		totalW += s.Size.Width
		totalH += s.Size.Height
	}
	return x <= -totalW || x >= totalW || y <= -totalH || y >= totalH
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
