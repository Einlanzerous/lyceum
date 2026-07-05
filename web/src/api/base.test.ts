import { afterEach, describe, expect, it } from 'vitest'
import {
  __resetServerCache,
  __setDefaultServer,
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
  __setDefaultServer(null)
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

describe('baked default server (VITE_LYCEUM_DEFAULT_SERVER — "my library" build)', () => {
  it('is used when no server is saved, so first run is zero-config', () => {
    __setNativeShell(true)
    __setDefaultServer('http://home.lan:8080')
    __resetServerCache()
    expect(hasBackend()).toBe(true)
    expect(apiBase()).toBe('http://home.lan:8080')
    expect(apiUrl('/library')).toBe('http://home.lan:8080/library')
  })

  it('is overridden by a saved server URL', () => {
    __setNativeShell(true)
    __setDefaultServer('http://home.lan:8080')
    setServerUrl('https://reader.example.com')
    expect(apiBase()).toBe('https://reader.example.com')
  })

  it('is reverted to (not blanked) when the saved URL is cleared', () => {
    __setNativeShell(true)
    __setDefaultServer('http://home.lan:8080')
    setServerUrl('https://reader.example.com')
    setServerUrl('   ')
    expect(getServerUrl()).toBe('http://home.lan:8080')
    expect(hasBackend()).toBe(true)
  })

  it('is normalized (trailing slash stripped) before use', () => {
    __setNativeShell(true)
    __setDefaultServer('http://home.lan:8080/')
    __resetServerCache()
    // Mirrors how BUILD_DEFAULT_SERVER is normalized at load — no double slash.
    expect(apiUrl('/sync')).toBe('http://home.lan:8080/sync')
  })

  it('is ignored in web mode (calls stay same-origin relative)', () => {
    __setDefaultServer('http://home.lan:8080')
    __resetServerCache()
    expect(isNativeShell()).toBe(false)
    expect(apiBase()).toBe('')
    expect(apiUrl('/library')).toBe('/library')
  })
})

describe('normalizeServerUrl', () => {
  it('trims whitespace and trailing slashes', () => {
    expect(normalizeServerUrl('  http://home.lan:8080/  ')).toBe('http://home.lan:8080')
    expect(normalizeServerUrl('http://home.lan:8080///')).toBe('http://home.lan:8080')
    expect(normalizeServerUrl('http://home.lan:8080')).toBe('http://home.lan:8080')
  })
})
