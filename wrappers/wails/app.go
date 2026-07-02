package main

import "context"

// App is the Wails application backend. The reader is a pure client of the
// remote Lyceum server (all state lives there and syncs over HTTP), so there
// are no bound Go methods here yet — the struct holds the Wails runtime context
// for any future native hooks (e.g. a system file picker for uploads).
type App struct {
	ctx context.Context
}

// NewApp constructs the App.
func NewApp() *App { return &App{} }

// startup is invoked by Wails once the runtime is ready; it captures the
// context Wails-runtime calls require.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}
