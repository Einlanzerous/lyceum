// Builds the web reader in native mode and copies its bundle into ./www, which
// Capacitor packages into the APK (LYCM-300). Run via `npm run copy-web` (and
// transitively by `npm run sync`). Uses Node APIs so it is cross-platform.
import { cpSync, rmSync, existsSync } from 'node:fs'
import { execSync } from 'node:child_process'

const webDir = '../../web'
const src = `${webDir}/dist`
const dest = 'www'

console.log('copy-web: building the web SPA in native mode…')
execSync('npm run build:native', { cwd: webDir, stdio: 'inherit' })

if (!existsSync(src)) {
  console.error(`copy-web: ${src} not found after build`)
  process.exit(1)
}

rmSync(dest, { recursive: true, force: true })
cpSync(src, dest, { recursive: true })
console.log(`copy-web: copied ${src} -> ${dest}`)
