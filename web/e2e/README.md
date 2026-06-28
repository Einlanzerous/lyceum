# Reader smoke (e2e)

epub.js renders into a real iframe and is not meaningfully testable under
jsdom/vitest, so the rendering path (LYCM-204/205/206) is covered here with a
Playwright smoke driving the **system Chrome** (`channel: 'chrome'` — no bundled
browser download).

## Run

The repo's `testdata` EPUBs are single-page, so the specs need a multi-page
book — `make-fixture.mjs` generates one. The backend content-addresses uploads,
so pass a unique tag to get a book with **no stored position** (full navigation
range, clean restore assertions).

```sh
# 1. Backend on an alternate port (8080 may be taken):
LYCEUM_ADDR=:8099 LYCEUM_DATA_DIR=$(mktemp -d) make -C .. run &   # from web/

# 2. Dev server proxying the API to that backend, on a fixed port:
LYCEUM_BACKEND=http://localhost:8099 npm run dev -- --port 5180 --strictPort &

# 3. Generate + upload a fresh multi-page book, capture its id:
node e2e/make-fixture.mjs /tmp/sample.epub "run-$(date +%s)"
ID=$(curl -sF file=@/tmp/sample.epub http://localhost:8099/upload | jq .id)

# 4. Run the specs against it:
E2E_BOOK_ID=$ID npm run test:e2e
```

Override `PLAYWRIGHT_BASE_URL` / `E2E_BOOK_ID` to point at a different stack.
The specs share one book's reading position and run serially (`workers: 1`).
