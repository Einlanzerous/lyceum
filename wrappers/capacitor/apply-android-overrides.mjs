// Applies the Lyceum-specific Android tweaks the generated project can't carry
// on its own (LYCM-300): cleartext HTTP so the reader can reach a plain-http
// home server on the LAN. Idempotent — safe to run after every `cap sync`.
// Wired into the npm `sync`/`build:apk` scripts. No-ops (with a notice) if the
// android/ project hasn't been generated yet (`npm run add:android`).
import { copyFileSync, existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs'

const manifestPath = 'android/app/src/main/AndroidManifest.xml'
if (!existsSync(manifestPath)) {
  console.log('apply-android-overrides: no android/ project yet — run `npm run add:android` first. Skipping.')
  process.exit(0)
}

// 1. Drop the network security config into the generated res/xml.
const xmlDir = 'android/app/src/main/res/xml'
mkdirSync(xmlDir, { recursive: true })
copyFileSync(
  'android-overrides/network_security_config.xml',
  `${xmlDir}/network_security_config.xml`,
)

// 2. Reference it from the <application> element and allow cleartext. Insert the
// attributes right after the opening <application tag, once.
let manifest = readFileSync(manifestPath, 'utf8')
if (manifest.includes('android:networkSecurityConfig')) {
  console.log('apply-android-overrides: manifest already patched.')
} else {
  manifest = manifest.replace(
    '<application',
    '<application\n        android:networkSecurityConfig="@xml/network_security_config"\n        android:usesCleartextTraffic="true"',
  )
  writeFileSync(manifestPath, manifest)
  console.log('apply-android-overrides: patched AndroidManifest for cleartext HTTP.')
}
