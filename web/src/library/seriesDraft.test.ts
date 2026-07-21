import { beforeEach, describe, expect, it } from 'vitest'
import { clearSeriesDraft, loadSeriesDraft, saveSeriesDraft } from './seriesDraft'

beforeEach(() => localStorage.clear())

describe('seriesDraft', () => {
  it('round-trips a draft per (batch, candidate)', () => {
    saveSeriesDraft(1, 10, { name: 'The Expanse', index: 2 })
    expect(loadSeriesDraft(1, 10)).toEqual({ name: 'The Expanse', index: 2 })
    // Different candidate / batch are isolated.
    expect(loadSeriesDraft(1, 11)).toBeNull()
    expect(loadSeriesDraft(2, 10)).toBeNull()
  })

  it('keeps drafts for many books in the same batch independent', () => {
    saveSeriesDraft(1, 10, { name: 'The Expanse', index: 1 })
    saveSeriesDraft(1, 11, { name: 'Foundation', index: null })
    expect(loadSeriesDraft(1, 10)).toEqual({ name: 'The Expanse', index: 1 })
    expect(loadSeriesDraft(1, 11)).toEqual({ name: 'Foundation', index: null })
  })

  it('removes the entry when a draft is emptied', () => {
    saveSeriesDraft(1, 10, { name: 'The Expanse', index: 2 })
    saveSeriesDraft(1, 10, { name: '  ', index: null })
    expect(loadSeriesDraft(1, 10)).toBeNull()
  })

  it('clears a single draft without touching its siblings', () => {
    saveSeriesDraft(1, 10, { name: 'The Expanse', index: 1 })
    saveSeriesDraft(1, 11, { name: 'Foundation', index: 1 })
    clearSeriesDraft(1, 10)
    expect(loadSeriesDraft(1, 10)).toBeNull()
    expect(loadSeriesDraft(1, 11)).toEqual({ name: 'Foundation', index: 1 })
  })

  it('survives corrupt storage without throwing', () => {
    localStorage.setItem('lyceum.ingest.series.1', '{not json')
    expect(loadSeriesDraft(1, 10)).toBeNull()
    // And a save recovers cleanly.
    saveSeriesDraft(1, 10, { name: 'Mistborn', index: 1 })
    expect(loadSeriesDraft(1, 10)).toEqual({ name: 'Mistborn', index: 1 })
  })
})
