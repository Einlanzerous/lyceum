// Per-book series drafts for the ingest verify screen (LYCM-95).
//
// A reviewer fills in each book's series as they work through a batch, then
// confirms. The half-entered assignment has to survive both switching to another
// book in the queue and a full page reload — but it is deliberately
// *device-local*, a resume aid rather than shared state, so it lives in
// localStorage keyed by batch (open the batch on another device and the drafts
// simply aren't there, which is fine). Confirming persists the series properly
// through the API; this is only the pre-confirm scratch, cleared once the book
// is shelved. There is intentionally no carry-over or auto-increment between
// books — each book's series is its own, entered explicitly.

export interface SeriesDraft {
  name: string
  index: number | null
}

const KEY_PREFIX = 'lyceum.ingest.series.'

function key(batchId: number): string {
  return `${KEY_PREFIX}${batchId}`
}

function readBatch(batchId: number): Record<string, SeriesDraft> {
  try {
    const raw = localStorage.getItem(key(batchId))
    const parsed: unknown = raw ? JSON.parse(raw) : null
    return parsed && typeof parsed === 'object' ? (parsed as Record<string, SeriesDraft>) : {}
  } catch {
    // Corrupt/absent storage → no drafts. A lost scratch note is a minor thing.
    return {}
  }
}

function writeBatch(batchId: number, drafts: Record<string, SeriesDraft>): void {
  try {
    if (Object.keys(drafts).length === 0) localStorage.removeItem(key(batchId))
    else localStorage.setItem(key(batchId), JSON.stringify(drafts))
  } catch {
    // localStorage can be unavailable or full; dropping the draft is acceptable.
  }
}

/** This candidate's saved draft, or null when nothing was entered for it. */
export function loadSeriesDraft(batchId: number, candidateId: number): SeriesDraft | null {
  return readBatch(batchId)[candidateId] ?? null
}

/**
 * Persist one candidate's draft — or, when it's empty (no name and no index),
 * remove it so cleared fields don't linger in storage.
 */
export function saveSeriesDraft(batchId: number, candidateId: number, draft: SeriesDraft): void {
  const drafts = readBatch(batchId)
  if (!draft.name.trim() && draft.index == null) delete drafts[candidateId]
  else drafts[candidateId] = { name: draft.name, index: draft.index }
  writeBatch(batchId, drafts)
}

/** Drop one candidate's draft, e.g. once it has been confirmed. */
export function clearSeriesDraft(batchId: number, candidateId: number): void {
  const drafts = readBatch(batchId)
  if (candidateId in drafts) {
    delete drafts[candidateId]
    writeBatch(batchId, drafts)
  }
}
