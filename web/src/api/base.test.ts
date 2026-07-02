import { afterEach, describe, expect, it } from 'vitest'
import {
  __resetServerCache,
  __setNativeShell,
  apiBase,
  apiUrl,
  getServerUrl,
  hasBackend,
  isNativeShell,
  normalizeServerUrl,
  setServerUrl,
} from './base'

afterEach(() => {
  __setNativeShell(null)
  __resetServerCache()
  localStorage.clear()
})

describe('web mode (default build)', () => {
  it('is not a native shell and uses relative URLs', () => {
    expect(isNativeShell()).toBe(false)
    expect(apiBase()).toBe('')
    expect(apiUrl('/library')).toBe('/library')
  })

  it('always has a backend regardless of any stored server URL', () => {
    setServerUrl('http://ignored:8080')
    expect(hasBackend()).toBe(true)
    // Still relative — web mode never prefixes the stored value.
    expect(apiUrl('/sync')).toBe('/sync')
  })
})

describe('native shell mode', () => {
  it('reports no backend until a server is configured', () => {
    __setNativeShell(true)
    expect(hasBackend()).toBe(false)
    // With no server, calls fall back to relative (and will fail) — the UI gates this.
    expect(apiUrl('/library')).toBe('/library')
  })

  it('prefixes every API path with the configured server', () => {
    __setNativeShell(true)
    setServerUrl('http://home.lan:8080')
    expect(hasBackend()).toBe(true)
    expect(apiBase()).toBe('http://home.lan:8080')
    expect(apiUrl('/library')).toBe('http://home.lan:8080/library')
    expect(apiUrl('/books/7/file')).toBe('http://home.lan:8080/books/7/file')
  })

  it('persists the server across cache resets (localStorage round-trip)', () => {
    __setNativeShell(true)
    setServerUrl('https://reader.example.com')
    __resetServerCache()
    expect(getServerUrl()).toBe('https://reader.example.com')
  })

  it('clears the server when set to empty', () => {
    __setNativeShell(true)
    setServerUrl('http://home.lan:8080')
    setServerUrl('   ')
    expect(getServerUrl()).toBe('')
    expect(hasBackend()).toBe(false)
  })
})

describe('normalizeServerUrl', () => {
  it('trims whitespace and trailing slashes', () => {
    expect(normalizeServerUrl('  http://home.lan:8080/  ')).toBe('http://home.lan:8080')
    expect(normalizeServerUrl('http://home.lan:8080///')).toBe('http://home.lan:8080')
    expect(normalizeServerUrl('http://home.lan:8080')).toBe('http://home.lan:8080')
  })
})
