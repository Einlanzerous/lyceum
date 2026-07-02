import type { CapacitorConfig } from '@capacitor/cli'

// Capacitor (Android) shell for the Lyceum reader (LYCM-300). It wraps the same
// TypeScript SPA as the web build (copied into ./www by copy-web.mjs) and talks
// to a remote Lyceum server the user configures on first run.
const config: CapacitorConfig = {
  appId: 'com.lyceum.reader',
  appName: 'Lyceum',
  webDir: 'www',
  server: {
    // Serve the app from the *http* scheme. Home Lyceum servers commonly run
    // plain HTTP on the LAN; with the default https scheme, calls to an http
    // backend are blocked as mixed content. http://localhost is the app origin
    // and is in the backend CORS allowlist (internal/api.DefaultCORSOrigins).
    // Cleartext to the LAN backend is then enabled via the Android network
    // security config — see README and android-overrides/.
    androidScheme: 'http',
  },
}

export default config
