import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  ApiError,
  bookFileUrl,
  coverUrl,
  getPosition,
  listLibrary,
  putPosition,
  putPositionKeepalive,
  uploadBook,
} from './client'
import type { Book, Position } from './types'

function mockFetch(impl: (url: string, init?: RequestInit) => Response | Promise<Response>) {
  const fn = vi.fn(impl as never)
  vi.stubGlobal('fetch', fn)
  return fn
}

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function textResponse(status: number, body: string): Response {
  return new Response(body, { status, headers: { 'Content-Type': 'text/plain' } })
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('url helpers', () => {
  it('build relative cover and file URLs', () => {
    expect(coverUrl(7)).toBe('/books/7/cover')
    expect(bookFileUrl(7)).toBe('/books/7/file')
  })
})

describe('listLibrary', () => {
  it('returns the parsed Book[]', async () => {
    const books: Book[] = [{ id: 1, title: 'A', author: 'B', cover_url: '/books/1/cover' }]
    mockFetch(() => jsonResponse(200, books))
    await expect(listLibrary()).resolves.toEqual(books)
  })

  it('throws ApiError carrying the plain-text body on failure', async () => {
    mockFetch(() => textResponse(500, 'internal server error'))
    await expect(listLibrary()).rejects.toMatchObject({
      name: 'ApiError',
      status: 500,
      message: 'internal server error',
    })
  })
})

describe('uploadBook', () => {
  it('POSTs multipart with field "file" and returns the created book', async () => {
    const created: Book = { id: 9, title: 'New', author: 'Auth', cover_url: '/books/9/cover' }
    const fetchFn = mockFetch((_url, init) => {
      expect(init?.method).toBe('POST')
      expect(init?.body).toBeInstanceOf(FormData)
      expect((init?.body as FormData).get('file')).toBeInstanceOf(File)
      return jsonResponse(201, created)
    })
    const file = new File([new Uint8Array([1, 2, 3])], 'book.epub', {
      type: 'application/epub+zip',
    })
    await expect(uploadBook(file)).resolves.toEqual(created)
    expect(fetchFn).toHaveBeenCalledWith('/upload', expect.objectContaining({ method: 'POST' }))
  })

  it('surfaces a 409 duplicate as an ApiError', async () => {
    mockFetch(() => textResponse(409, 'book already exists'))
    const file = new File(['x'], 'dupe.epub')
    await expect(uploadBook(file)).rejects.toMatchObject({ status: 409, message: 'book already exists' })
  })
})

describe('getPosition', () => {
  it('maps 404 to null (no saved position)', async () => {
    mockFetch(() => textResponse(404, 'no reading position'))
    await expect(getPosition(1, 'dev-1')).resolves.toBeNull()
  })

  it('passes book_id and device_id as query params', async () => {
    const pos: Position = {
      book_id: 1,
      device_id: 'dev-1',
      cfi: 'epubcfi(/6/4!/4/2)',
      progress: 0.5,
      updated_at: '2026-06-27T00:00:00.000Z',
    }
    const fetchFn = mockFetch(() => jsonResponse(200, pos))
    await expect(getPosition(1, 'dev-1')).resolves.toEqual(pos)
    const calledUrl = fetchFn.mock.calls[0]![0] as string
    expect(calledUrl).toContain('book_id=1')
    expect(calledUrl).toContain('device_id=dev-1')
  })

  it('throws ApiError on non-404 failures', async () => {
    mockFetch(() => textResponse(400, 'book_id is required'))
    await expect(getPosition(0, 'dev-1')).rejects.toBeInstanceOf(ApiError)
  })
})

describe('putPosition', () => {
  it('PUTs JSON and defaults updated_at when omitted', async () => {
    let sentBody: Record<string, unknown> = {}
    mockFetch((_url, init) => {
      expect(init?.method).toBe('PUT')
      sentBody = JSON.parse(init?.body as string)
      return jsonResponse(200, sentBody)
    })
    await putPosition({
      book_id: 2,
      device_id: 'dev-1',
      cfi: 'epubcfi(/6/4!/4/2)',
      progress: 0.25,
    })
    expect(sentBody.updated_at).toEqual(expect.any(String))
    expect(Number.isNaN(Date.parse(sentBody.updated_at as string))).toBe(false)
  })

  it('preserves a caller-supplied updated_at', async () => {
    const ts = '2026-06-27T12:00:00.000Z'
    let sentBody: Record<string, unknown> = {}
    mockFetch((_url, init) => {
      sentBody = JSON.parse(init?.body as string)
      return jsonResponse(200, sentBody)
    })
    await putPosition({
      book_id: 2,
      device_id: 'dev-1',
      cfi: 'epubcfi(/6/4!/4/2)',
      progress: 0.25,
      updated_at: ts,
    })
    expect(sentBody.updated_at).toBe(ts)
  })
})

describe('putPositionKeepalive', () => {
  it('issues a PUT with keepalive:true and the position payload', () => {
    let url = ''
    let init: RequestInit | undefined
    const fetchFn = mockFetch((u, i) => {
      url = u
      init = i
      return jsonResponse(200, {})
    })
    putPositionKeepalive({
      book_id: 5,
      device_id: 'dev-1',
      cfi: 'epubcfi(/6/4!/4/2)',
      progress: 0.7,
    })
    expect(fetchFn).toHaveBeenCalledOnce()
    expect(url).toBe('/sync')
    expect(init?.method).toBe('PUT')
    expect(init?.keepalive).toBe(true)
    const body = JSON.parse(init?.body as string)
    expect(body).toMatchObject({ book_id: 5, device_id: 'dev-1', progress: 0.7 })
    expect(typeof body.updated_at).toBe('string')
  })
})
