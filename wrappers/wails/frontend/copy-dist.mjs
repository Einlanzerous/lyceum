// Builds the web reader in native mode and copies its bundle into this Wails
// project's frontend/dist, which main.go embeds (LYCM-300). Invoked as the
// single `frontend:build` command in wails.json — kept to ONE command (no shell
// `&&`) because Wails runs frontend:build without a shell, so a chained command
// would pass `&&` to the next tool as a literal argument. Uses `node:` APIs
// rather than shelling out, so it works on the Windows and Linux build hosts
// alike; bun is the runner (LYCM-71). Run from this `frontend` directory
// (wails.json's frontend:dir).
import { cpSync, rmSync, existsSync } from 'node:fs'
import { execSync } from 'node:child_process'

const webDir = '../../../web'
const src = `${webDir}/dist`
const dest = 'dist'

console.log('copy-dist: building the web SPA in native mode…')
// Honour the Makefile's `BUN ?= bun` override so a non-PATH bun (BUN=/opt/bun/
// bin/bun make wails-windows) is used here too, not just for the make steps.
const bun = process.env.BUN ?? 'bun'
execSync(`${bun} run build:native`, { cwd: webDir, stdio: 'inherit' })

if (!existsSync(src)) {
  console.error(`copy-dist: ${src} not found after build`)
  process.exit(1)
}

rmSync(dest, { recursive: true, force: true })
cpSync(src, dest, { recursive: true })
console.log(`copy-dist: copied ${src} -> ${dest}`)
