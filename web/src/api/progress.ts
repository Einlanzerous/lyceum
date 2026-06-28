// Pure helpers for reading-progress values (0..1). Kept separate from the
// client so they're trivially unit-testable.

/** Clamp an arbitrary number into the valid 0..1 progress range. */
export function clampProgress(value: number): number {
  if (Number.isNaN(value)) return 0
  if (value <= 0) return 0
  if (value >= 1) return 1
  return value
}

/** Format a 0..1 progress fraction as a whole-percent label, e.g. "42%". */
export function formatProgress(value: number): string {
  return `${Math.round(clampProgress(value) * 100)}%`
}
