// Typed client for the ISBN ingest batch-review surface (LYCM-603), mirroring
// internal/api/ingest_batch.go. A batch of scanned ISBNs resolves to candidates
// the desktop verifies (pick an edition, confirm, skip) before they join
// inventory / the library. Every URL is resolved through apiUrl() like the rest
// of the client (same-origin on web, remote backend in the native shells).

import { apiUrl } from './base'
import { readError } from './client'

/** One candidate match for a scanned ISBN (a resolved book edition). */
export interface Edition {
  id: string
  isbn13?: string
  title: string
  author?: string
  publisher?: string
  year?: string
  pages?: number
  language?: string
  cover_url?: string
}

export type CandidateStatus =
  'ready' | 'review' | 'no_match' | 'duplicate' | 'confirmed' | 'skipped'

export type ScanSource = 'camera' | 'manual' | 'title'

/** A single scan resolved to a review row. */
export interface Candidate {
  id: number
  batch_id: number
  isbn: string
  source: ScanSource
  status: CandidateStatus
  confidence: number
  chosen_edition_id?: string
  editions: Edition[]
  title?: string
  author?: string
  cover_url?: string
  series?: string
  series_index?: number
  captured_at?: string
}

/** A batch of scans awaiting review, with a per-status histogram. */
export interface Batch {
  id: number
  source_device?: string
  status: 'open' | 'confirmed' | 'discarded'
  created_at: string
  counts: Record<string, number>
  candidates?: Candidate[]
}

/** One scan in a batch-create payload. */
export interface ScanInput {
  isbn: string
  source?: ScanSource
  captured_at?: string
}

async function getJSON<T>(path: string): Promise<T> {
  const res = await fetch(apiUrl(path))
  if (!res.ok) throw await readError(res)
  return (await res.json()) as T
}

async function sendJSON<T>(path: string, body: unknown, method = 'POST'): Promise<T> {
  const res = await fetch(apiUrl(path), {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!res.ok) throw await readError(res)
  return (await res.json()) as T
}

/** GET /ingest/batches — every review batch (headers + counts, no candidates). */
export function listBatches(): Promise<Batch[]> {
  return getJSON<Batch[]>('/ingest/batches')
}

/** GET /ingest/batches/{id} — a batch with all its candidates. */
export function getBatch(id: number): Promise<Batch> {
  return getJSON<Batch>(`/ingest/batches/${id}`)
}

/** POST /ingest/batches — upload scans and resolve them into a new batch. */
export function createBatch(scans: ScanInput[], sourceDevice = ''): Promise<Batch> {
  return sendJSON<Batch>('/ingest/batches', { source_device: sourceDevice, scans })
}

/** POST /ingest/batches/{id}/candidates — append one scan/pick to a batch. */
export function addCandidate(
  batchId: number,
  isbn: string,
  source: ScanSource = 'title',
): Promise<Candidate> {
  return sendJSON<Candidate>(`/ingest/batches/${batchId}/candidates`, { isbn, source }, 'POST')
}

/** POST /ingest/candidates/{id}/pick — resolve a review row to one edition. */
export function pickEdition(candidateId: number, editionId: string): Promise<Candidate> {
  return sendJSON<Candidate>(`/ingest/candidates/${candidateId}/pick`, { edition_id: editionId })
}

export interface ConfirmResult {
  candidate: Candidate
  inventory: { id: number; isbn: string; title?: string; author?: string; state: string }
}

/** POST /ingest/candidates/{id}/confirm — shelve one candidate into inventory. */
export function confirmCandidate(
  candidateId: number,
  series = '',
  seriesIndex = 0,
): Promise<ConfirmResult> {
  return sendJSON<ConfirmResult>(`/ingest/candidates/${candidateId}/confirm`, {
    series,
    series_index: seriesIndex,
  })
}

/** POST /ingest/candidates/{id}/skip — drop a candidate from review. */
export async function skipCandidate(candidateId: number): Promise<void> {
  const res = await fetch(apiUrl(`/ingest/candidates/${candidateId}/skip`), { method: 'POST' })
  if (!res.ok) throw await readError(res)
}

export interface ConfirmReadyResult {
  confirmed: number
  batch: Batch
}

/** POST /ingest/batches/{id}/confirm-ready — shelve every ready candidate. */
export function confirmReady(batchId: number): Promise<ConfirmReadyResult> {
  return sendJSON<ConfirmReadyResult>(`/ingest/batches/${batchId}/confirm-ready`, {})
}

/** GET /ingest/search?q= — editions matching a free-text title (add-by-title). */
export async function searchEditions(query: string): Promise<Edition[]> {
  const res = await getJSON<{ editions: Edition[] }>(
    `/ingest/search?q=${encodeURIComponent(query)}`,
  )
  return res.editions
}
