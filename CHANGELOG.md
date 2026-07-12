# Changelog

## [1.3.2](https://github.com/Einlanzerous/lyceum/compare/v1.3.1...v1.3.2) (2026-07-12)


### Bug Fixes

* **ingest:** dispatch acquirer.Want in the background so confirm returns immediately (LYCM-79) ([#39](https://github.com/Einlanzerous/lyceum/issues/39)) ([9908a91](https://github.com/Einlanzerous/lyceum/commit/9908a912791bbd3c83c8114079dee47b7c440aab))
* **ingest:** match source_path case-insensitively so re-cased files update in place (LYCM-68) ([#37](https://github.com/Einlanzerous/lyceum/issues/37)) ([021b705](https://github.com/Einlanzerous/lyceum/commit/021b7059f48e13b561f3843afa2f2025f3f3f7a6))
* **watch:** surface non-EPUB book landings instead of silently skipping them (LYCM-77) ([#38](https://github.com/Einlanzerous/lyceum/issues/38)) ([d6fa658](https://github.com/Einlanzerous/lyceum/commit/d6fa658cb4f30ab45e3f3057b23934aa29160ec1))
* **web:** equal grid tracks — minmax(0,1fr) so a long title can't widen a column (LYCM-80) ([#36](https://github.com/Einlanzerous/lyceum/issues/36)) ([1204725](https://github.com/Einlanzerous/lyceum/commit/1204725b5724255324738af92827ff61953959d7))

## [1.3.1](https://github.com/Einlanzerous/lyceum/compare/v1.3.0...v1.3.1) (2026-07-12)


### Bug Fixes

* **ingest:** auto-close a batch when its last candidate is resolved ([#35](https://github.com/Einlanzerous/lyceum/issues/35)) ([8b7a433](https://github.com/Einlanzerous/lyceum/commit/8b7a4337946c5e5c17e4bbb5d7c918142dbf2a3b))
* **mobile:** 16 KB page-align native libs — bump mobile_scanner to 7.2.0 (LYCM-73) ([#33](https://github.com/Einlanzerous/lyceum/issues/33)) ([cd62c46](https://github.com/Einlanzerous/lyceum/commit/cd62c4678e40df16e92c068d3d27ad8635509bec))

## [1.3.0](https://github.com/Einlanzerous/lyceum/compare/v1.2.1...v1.3.0) (2026-07-12)


### Features

* **acquire:** live Bindery acquirer (grab w/ bookId via searchOnAdd) (LYCM-35) ([#28](https://github.com/Einlanzerous/lyceum/issues/28)) ([598b18d](https://github.com/Einlanzerous/lyceum/commit/598b18dbc6d8e5de6110267f5aeece7b6c498a85))
* **ingest:** ISBN batch review + verify admin (LYCM-603) ([#26](https://github.com/Einlanzerous/lyceum/issues/26)) ([cbc01bf](https://github.com/Einlanzerous/lyceum/commit/cbc01bf8957603271100c136d0cbe432d4d7e6bc))
* **ingest:** normalize cover art at ingest (LYCM-65) ([#30](https://github.com/Einlanzerous/lyceum/issues/30)) ([d386d05](https://github.com/Einlanzerous/lyceum/commit/d386d05ba1db8c0b061d01731f1518513eb6597c))
* **ingest:** QC review/override queue for new ingests (LYCM-58) ([#31](https://github.com/Einlanzerous/lyceum/issues/31)) ([2dc64c9](https://github.com/Einlanzerous/lyceum/commit/2dc64c9494b6530852594f708481a5de36283082))
* **inventory:** reconcile print↔ebook ISBNs by work (LYCM-35) ([#29](https://github.com/Einlanzerous/lyceum/issues/29)) ([b5463b8](https://github.com/Einlanzerous/lyceum/commit/b5463b8d12879f3f936456bc6bf253bc2b8d0c5e))
* **mobile:** in-app ISBN barcode scanning (LYCM-602/LYCM-34) ([#32](https://github.com/Einlanzerous/lyceum/issues/32)) ([0b56bca](https://github.com/Einlanzerous/lyceum/commit/0b56bcaa45a27f3a8283dd4a3974d412e4c3a87f))

## [1.2.1](https://github.com/Einlanzerous/lyceum/compare/v1.2.0...v1.2.1) (2026-07-10)


### Bug Fixes

* **sync:** resume from the furthest position across devices, not the latest write ([#24](https://github.com/Einlanzerous/lyceum/issues/24)) ([c4438d4](https://github.com/Einlanzerous/lyceum/commit/c4438d4e051dcb0f70dd40371d7e27f49ea6076f))

## [1.2.0](https://github.com/Einlanzerous/lyceum/compare/v1.1.0...v1.2.0) (2026-07-09)


### Features

* **api:** LYCM-66 book delete API + stable-identity folder ingest ([#17](https://github.com/Einlanzerous/lyceum/issues/17)) ([c83ae4e](https://github.com/Einlanzerous/lyceum/commit/c83ae4e23a6429bdd72c18a586c6f32fd1fd0258))
* **deploy:** LYCM-61 production image + GHCR publish ([#15](https://github.com/Einlanzerous/lyceum/issues/15)) ([7283cbd](https://github.com/Einlanzerous/lyceum/commit/7283cbdc90eb60db664641844a1d816bd4fab628))
* library sort, search, and series roll-up (LYCM-62/63/36) ([#19](https://github.com/Einlanzerous/lyceum/issues/19)) ([584818a](https://github.com/Einlanzerous/lyceum/commit/584818a588d1f693b5b8ca47589b5d4f46865c4b))
* **library:** mark books as read (manual finished flag) ([#23](https://github.com/Einlanzerous/lyceum/issues/23)) ([191f061](https://github.com/Einlanzerous/lyceum/commit/191f061987fbd634a7e942b1b9eb1ae347c78d80))
* **library:** polish series UI + restyle sort dropdown ([#20](https://github.com/Einlanzerous/lyceum/issues/20)) ([99dcf19](https://github.com/Einlanzerous/lyceum/commit/99dcf19bbc81137a179d45e7fed165924df5a06c))
* **library:** series card wears the cover of the volume you're on ([#22](https://github.com/Einlanzerous/lyceum/issues/22)) ([a982553](https://github.com/Einlanzerous/lyceum/commit/a9825533446431fd99ac159ab7994dc593026b1f))
* **mobile:** LYCM-60 richer library covers (larger 2-up grid tiles) ([#14](https://github.com/Einlanzerous/lyceum/issues/14)) ([6bd6275](https://github.com/Einlanzerous/lyceum/commit/6bd6275aefca103e98f34648b03aa60f89b20d60))


### Bug Fixes

* **ci:** LYCM-59 build Windows installer (makensis was missing on runner) ([#12](https://github.com/Einlanzerous/lyceum/issues/12)) ([c7edbfc](https://github.com/Einlanzerous/lyceum/commit/c7edbfc2e174c5e29da54014974d6a8724a016f2))
* **mobile:** LYCM-54 stop Android hang on unreachable backend + kill ephemeral dev port ([#16](https://github.com/Einlanzerous/lyceum/issues/16)) ([b1cc8af](https://github.com/Einlanzerous/lyceum/commit/b1cc8af8f1cf0e85b5bd8c44cefadcff89c8b7e7))

## [1.1.0](https://github.com/Einlanzerous/lyceum/compare/v1.0.0...v1.1.0) (2026-07-07)


### Features

* **mobile:** LYCM-700 app icon, privacy policy, robust versionCode ([#10](https://github.com/Einlanzerous/lyceum/issues/10)) ([d1ee57e](https://github.com/Einlanzerous/lyceum/commit/d1ee57e61290ba31da0da0d427437279031f6c51))

## 1.0.0 (2026-07-07)


### Features

* **backend:** LYCM-400 — ecosystem & agent integration (Phase 4) ([0899950](https://github.com/Einlanzerous/lyceum/commit/089995064e4e68383bd41a8cc1820f87edd7a176))
* **backend:** LYCM-601 — ISBN inventory + folder-ingest acquisition pipeline ([931b7d2](https://github.com/Einlanzerous/lyceum/commit/931b7d236998e8d8958bae19a3c322d0f2d035a3))
* **covers:** LYCM-56 fetch canonical cover art (Apple Books) + backfill tool ([0fa8d22](https://github.com/Einlanzerous/lyceum/commit/0fa8d226d82b7248f1c177d4d21a24019747bd11))
* **mobile:** LYCM-700 native Flutter Android app + shared reader/UX refinements ([e4eba11](https://github.com/Einlanzerous/lyceum/commit/e4eba11680101204337547a27ee0dbde05e5dda0))
* **mobile:** LYCM-700 signed Android release pipeline ([#9](https://github.com/Einlanzerous/lyceum/issues/9)) ([5128a47](https://github.com/Einlanzerous/lyceum/commit/5128a4750da72b7fd0a9c8b6385fe07a993d380c))
* **wails:** Lyceum brand-mark app icon ([1a82425](https://github.com/Einlanzerous/lyceum/commit/1a82425129c0c302f2eebe24b22c346a22af61d7))
* **web:** add favicon from the Lyceum brand mark ([6f3c21c](https://github.com/Einlanzerous/lyceum/commit/6f3c21c55b6284e9b2b737c8ed4fafe95b057dbd))
* **web:** default-server seam for a zero-config native build ([7d7aa1d](https://github.com/Einlanzerous/lyceum/commit/7d7aa1d048f0f16f9b7925d6409a0ee0e08c63c6))
* **web:** implement the Lyceum design system (dark/light, brass-on-charcoal) ([bf073b7](https://github.com/Einlanzerous/lyceum/commit/bf073b7386555984adfa199fb3eebc8d1962f799))
* **web:** in-app update check for the native shell ([430a90d](https://github.com/Einlanzerous/lyceum/commit/430a90d711c6334be232bcddc8edb117e495d5f7))
* **web:** in-reader quick-settings panel + clearer Contents icon ([377f9b1](https://github.com/Einlanzerous/lyceum/commit/377f9b1b9bc37825227765240b23ac8d9c4267bf))
* **web:** LYCM-501 — library theme toggle + Settings page scaffold ([ec65f43](https://github.com/Einlanzerous/lyceum/commit/ec65f4388efd696ee7e18b9294df381b0cfc2513))
* **web:** LYCM-502 — opt-in custom reading font ([cef4e2b](https://github.com/Einlanzerous/lyceum/commit/cef4e2b1e8b64852f354a857707a091e900d09e4))
* **wrappers:** LYCM-300 cross-platform shells + backend base-URL/CORS seam ([4a47b97](https://github.com/Einlanzerous/lyceum/commit/4a47b976ca1eaf794a15d8ad46c4ac4780a2df6b))


### Bug Fixes

* **library:** LYCM-56 uniform cover framing to match the cover source ([94bc300](https://github.com/Einlanzerous/lyceum/commit/94bc3002cf754a1106bc5115e71e5846931ab371))
* **web:** generate device_id without crypto.randomUUID in insecure contexts ([c59c7df](https://github.com/Einlanzerous/lyceum/commit/c59c7dfbebf2680bbd6dbc933c040de44817d33f))
* **web:** keep the hatch texture off real cover art ([08bb689](https://github.com/Einlanzerous/lyceum/commit/08bb689ba644c07eab7e53f2753e103da7cc5b6f))


### Maintenance

* gitignore local example_docs sample books ([d4d144a](https://github.com/Einlanzerous/lyceum/commit/d4d144afb60742f9135e796588da5c40b920e424))
* **wrappers:** remove superseded Capacitor Android shell ([bc004ab](https://github.com/Einlanzerous/lyceum/commit/bc004ab006ddcda6c84c7ed81c9439dbff7730b0))
