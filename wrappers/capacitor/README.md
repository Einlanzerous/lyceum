# Lyceum Android (Capacitor)

The sideloadable Android `.apk` shell for the Lyceum reader (LYCM-300). It wraps
the **same** TypeScript SPA as the web build and talks to a remote Lyceum server
the user configures on first launch (Settings → Connection). No backend ships in
the app.

## How it fits together

- `npm run copy-web` builds the SPA with `npm run build:native` (which sets
  `VITE_LYCEUM_TARGET=native`, so `web/src/api/base.ts` prefixes API calls with
  the configured server) and copies `web/dist` into `./www`.
- Capacitor packages `./www` into the APK and serves it from
  `http://localhost`. That origin is in the backend CORS allowlist
  (`internal/api.DefaultCORSOrigins`), and the http scheme (see
  `capacitor.config.ts`) keeps calls to an http home server from being blocked
  as mixed content.

## Prerequisites (on the build machine)

- Node 20+
- JDK 17, Android SDK + platform-tools, and Gradle (Android Studio installs all
  three). Set `ANDROID_HOME`.

## Build

From the repository root:

```sh
make android-apk            # installs deps, builds the SPA, syncs, assembles a debug APK
```

or step by step in this directory:

```sh
npm install
npm run add:android         # one-time: generates the android/ project
# one-time: wire up cleartext HTTP (see "Cleartext" below)
npm run build:apk           # copy-web + cap sync + gradlew assembleDebug
```

The debug APK lands at
`android/app/build/outputs/apk/debug/app-debug.apk`. Sideload it with
`adb install -r <path>` or transfer it to the device.

## Cleartext (reaching an http home server)

Android blocks plain-HTTP traffic by default. After `npm run add:android`, copy
the provided override into the generated project and reference it:

```sh
cp android-overrides/network_security_config.xml \
   android/app/src/main/res/xml/network_security_config.xml
```

Then on the `<application>` element in
`android/app/src/main/AndroidManifest.xml` add:

```xml
android:networkSecurityConfig="@xml/network_security_config"
android:usesCleartextTraffic="true"
```

If your server runs HTTPS, you can skip this and set `androidScheme: 'https'` in
`capacitor.config.ts` instead.

## Notes

- `android/` is generated and git-ignored; re-run `npm run add:android` on a
  fresh checkout.
- Release/signed APKs follow the standard Android signing flow
  (`assembleRelease` + a keystore) — out of scope for the sideload target here.
