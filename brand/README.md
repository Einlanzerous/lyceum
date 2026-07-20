# Lyceum brand assets

The Lyceum mark: a brass rounded-square rotated 45° into a diamond, on a dark
plate. The same mark the Android app (`mobile/lyceum/assets/brand/app_icon.png`),
the Wails desktop shell, and the web favicon (`web/public/favicon.svg`) all use —
kept here as the canonical, reusable copies.

| File | What it is | Use for |
|------|-----------|---------|
| `lyceum-logo.png` | 1024×1024 plate icon (brass diamond on the app's dark plate) | App-launcher tiles, store listings, anywhere a square raster logo is wanted. Mirrors the Android launcher icon. |
| `lyceum-mark.svg` | The bare diamond mark, stroke-only, theme-aware | Vector use — inline in a page, scaled small, or recoloured. |

## Cloudflare Access App Launcher (LYCM-803)

Lyceum sits behind Cloudflare Access (see the tunnel/edge setup in
`construct-server`). The Access **App Launcher** shows each app as a tile with a
logo. Point Lyceum's tile at this file's raw URL so the launcher renders the same
mark the apps use:

- Zero Trust dashboard → **Access → Applications → Lyceum → App Launcher →
  Custom logo URL**, set to:

  ```
  https://raw.githubusercontent.com/Einlanzerous/lyceum/main/brand/lyceum-logo.png
  ```

A raw GitHub URL is used deliberately: the App Launcher fetches the logo from
Cloudflare's edge, and Lyceum's own origin is behind the Access gate — so the
logo must live somewhere public and stable, not on `lyceum.zerogravity.industries`.
