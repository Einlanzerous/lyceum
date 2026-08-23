# Android release signing & distribution

`mobile-release.yml` builds a **signed** APK + AAB on every `v*` tag (the tags
release-please cuts), checks that neither has a server address baked into it,
attaches the APK to that GitHub Release, and — once a Play service account is
configured — pushes the AAB to the Play **closed testing (`alpha`)** track.

Release builds are signed with an **upload keystore** read from
`android/key.properties` (gitignored). When that file is absent (local dev, CI
debug builds) the build falls back to the debug key, so `flutter run --release`
still works — see `app/build.gradle.kts`.

## The store-build gate

Between the build and the upload, `tool/check_store_build.sh` reads the APK and
the AAB back and fails the job if either carries a server address (LYCM-104). A
public build has to arrive knowing nothing — the invite QR is what points it at a
library — and that property lives in the build *command*, so it cannot be held by
anything in the Dart source. Nothing ships past a failure: the gate sits ahead of
both the GitHub Release upload and the Play push.

If it ever fires, the fix is to take the `--dart-define` back out, not to widen
the guard. The one legitimate build that fails it on purpose is the sideloaded
family flavour — see the app README — and that one is not released from here.

One honest failure exists and is worth recognising quickly, because it lands at
tag time rather than on a PR: the origin check holds a list of the URLs a clean
Dart snapshot is expected to contain, and a **Flutter upgrade** can add a new
diagnostic link to it. That reads as `unexpected absolute URL(s) in the Dart
snapshot` naming an `api.flutter.dev`-shaped address rather than a server. Check
that is what it is, add it to `ALLOWED_ORIGINS` in the script, and re-run the
build for the same tag: Actions → *mobile-release* → *Run workflow* → `tag:
vX.Y.Z`. Nothing was published before the failure, so there is nothing to undo.

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

## Play Store closed-testing track

The AAB → Play step is **skipped** unless `PLAY_SERVICE_ACCOUNT_JSON` is set, so
the signed GitHub-Release APK works on its own. Note the track: the workflow
uploads to **`alpha`** (Play's *Closed testing*), so make the first manual upload
below on that same track — a track CI pushes to has to exist first. The browser steps below are one-time and can only be done by the Play
Developer account owner.

### A. Create the app (Play Console)

1. <https://play.google.com/console> → **Create app**. App name *Lyceum*, type
   *App*, **Free**, accept the declarations.
2. **Set up your app** → work through the required tasks: privacy policy URL, app
   access, ads (none), content rating questionnaire, target audience, data
   safety, government-apps = no. These gate even closed testing.
3. The package name `dev.dodson.lyceum` is claimed by the **first uploaded AAB**
   (next step) — there's no separate "register package" action.

### B. First AAB upload (manual — Google requires it)

1. **Testing → Closed testing → Alpha → Create new release** (the track
   `mobile-release.yml` uploads to).
2. On the first release Play offers **Play App Signing** — **accept it** (Google
   manages the app signing key; our `upload-keystore.jks` stays the upload key).
3. Upload the AAB built with the upload key:
   `flutter build appbundle --release` → `build/app/outputs/bundle/release/app-release.aab`.
4. Add a release name / notes, **Save → Review → Start rollout**. Add testers
   under the *Testers* tab and use the opt-in link.

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
AAB to the closed-testing track automatically (`r0adkll/upload-google-play`).
Promote closed → production from the Play Console when ready.

Before the production promotion, work through
[`store-listing.md`](store-listing.md): the listing has to say that Lyceum needs
a server and an invite, or the empty server field on first launch reads as a
broken app.

## Versioning

`versionName` comes from the tag (`v1.0.0` → `1.0.0`); `versionCode` is a Unix
timestamp, which Play's strictly-increasing requirement needs and a run number
cannot give — this workflow is reached both by `workflow_call` (where
`github.run_number` is the *caller's* counter) and by `workflow_dispatch` (its
own), so run numbers aren't monotonic across the two. pubspec stays at its dev
default — the tag is the source of truth for released builds.
