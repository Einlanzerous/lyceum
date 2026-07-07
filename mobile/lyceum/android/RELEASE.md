# Android release signing & distribution

`mobile-release.yml` builds a **signed** APK + AAB on every `v*` tag (the tags
release-please cuts), attaches the APK to that GitHub Release, and — once a Play
service account is configured — pushes the AAB to the Play **internal** track.

Release builds are signed with an **upload keystore** read from
`android/key.properties` (gitignored). When that file is absent (local dev, CI
debug builds) the build falls back to the debug key, so `flutter run --release`
still works — see `app/build.gradle.kts`.

## One-time: create the upload keystore

```sh
keytool -genkeypair -v \
  -keystore upload-keystore.jks -storetype PKCS12 \
  -alias upload -keyalg RSA -keysize 2048 -validity 10000
```

The default keystore type is **PKCS12**, which uses a single password for the
store *and* the key — `keytool` ignores a separate `-keypass`. So
`storePassword` and `keyPassword` are the same value (and the
`ANDROID_KEYSTORE_PASSWORD` / `ANDROID_KEY_PASSWORD` secrets below are identical).

Keep `upload-keystore.jks` **somewhere safe and private** (a password manager /
secrets vault). Never commit it — `**/*.jks` and `key.properties` are already
gitignored. This is the **upload** key, not the app signing key: with Play App
Signing (enrolled on first release) Google holds the real signing key, so a lost
upload key is recoverable by resetting it in the Play Console — but still treat
it as a secret. This is lyceum's **own** key — it is not shared with argosy or
any other app.

### Local release builds

Create `android/key.properties` (next to `app/`) pointing at the keystore:

```properties
storeFile=/absolute/path/to/upload-keystore.jks
storePassword=********
keyAlias=upload
keyPassword=********
```

Then `flutter build apk --release` / `flutter build appbundle --release`.

## GitHub secrets (Repo → Settings → Secrets and variables → Actions)

Required for the signed Android build:

| Secret | Value |
| --- | --- |
| `ANDROID_KEYSTORE_BASE64` | `base64 -w0 upload-keystore.jks` (the keystore, base64-encoded) |
| `ANDROID_KEYSTORE_PASSWORD` | the `storePassword` |
| `ANDROID_KEY_ALIAS` | the alias (`upload` above) |
| `ANDROID_KEY_PASSWORD` | same value as `ANDROID_KEYSTORE_PASSWORD` (PKCS12) |

The workflow decodes the keystore to `android/app/upload-keystore.jks` and
writes `android/key.properties` at build time. Without `ANDROID_KEYSTORE_BASE64`
the Android release job fails fast with a pointer here.

Set them in one shot with `gh` (run from the repo root):

```sh
base64 -w0 upload-keystore.jks | gh secret set ANDROID_KEYSTORE_BASE64
printf '%s' "$STOREPASS" | gh secret set ANDROID_KEYSTORE_PASSWORD
printf '%s' "upload"     | gh secret set ANDROID_KEY_ALIAS
printf '%s' "$STOREPASS" | gh secret set ANDROID_KEY_PASSWORD
```

## How a release triggers the build

release-please cuts the `v*` tag (via the App-minted token in `release.yml`).
`mobile-release` is a **reusable workflow** invoked by `release.yml` (gated on
`release_created`) in the same push-to-main run, so the signed build runs exactly
once regardless of whether the App-token tag re-fires a `push: tags` event. To
rebuild/re-attach a signed artifact for an existing tag without cutting a
release: Actions → *mobile-release* → *Run workflow* → `tag: v1.0.0`.

## Play Store internal track

The AAB → Play internal-track step is **skipped** unless
`PLAY_SERVICE_ACCOUNT_JSON` is set, so the signed GitHub-Release APK works on its
own. The browser steps below are one-time and can only be done by the Play
Developer account owner.

### A. Create the app (Play Console)

1. <https://play.google.com/console> → **Create app**. App name *Lyceum*, type
   *App*, **Free**, accept the declarations.
2. **Set up your app** → work through the required tasks: privacy policy URL, app
   access, ads (none), content rating questionnaire, target audience, data
   safety, government-apps = no. These gate even internal testing.
3. The package name `dev.dodson.lyceum` is claimed by the **first uploaded AAB**
   (next step) — there's no separate "register package" action.

### B. First AAB upload (manual — Google requires it)

1. **Testing → Internal testing → Create new release**.
2. On the first release Play offers **Play App Signing** — **accept it** (Google
   manages the app signing key; our `upload-keystore.jks` stays the upload key).
3. Upload the AAB built with the upload key:
   `flutter build appbundle --release` → `build/app/outputs/bundle/release/app-release.aab`.
4. Add a release name / notes, **Save → Review → Start rollout to Internal
   testing**. Add testers under the *Testers* tab and use the opt-in link.

### C. Service account → enable CI auto-upload

The `PLAY_SERVICE_ACCOUNT_JSON` secret is the full JSON of a Google service
account granted *Release to testing tracks* in the Play Console. Lyceum reuses
the existing `construct-server@zero-gravity-industries.iam.gserviceaccount.com`
service account (already used to publish other Play apps); it just needs to be
**granted access to this app** in Play Console → Users & permissions if it isn't
already at the account level. Set the secret with:

```sh
gh secret set PLAY_SERVICE_ACCOUNT_JSON < /path/to/service-account.json
```

Once `PLAY_SERVICE_ACCOUNT_JSON` is set, every release-please release uploads the
AAB to the internal track automatically (`r0adkll/upload-google-play`). Promote
internal → closed → production from the Play Console when ready.

## Versioning

`versionName` comes from the tag (`v1.0.0` → `1.0.0`); `versionCode` is the CI
run number (monotonic, as Play requires). pubspec stays at its dev default — the
tag is the source of truth for released builds.
