import { describe, expect, it } from 'vitest'
import { extractInviteToken, inviteSignInUrl } from './invite'

describe('extractInviteToken', () => {
  it('returns a bare token untouched', () => {
    expect(extractInviteToken('lyc_abc123DEF-_')).toBe('lyc_abc123DEF-_')
  })

  it('strips wrapping whitespace and newlines from a pasted key', () => {
    expect(extractInviteToken('  lyc_abc123\n')).toBe('lyc_abc123')
  })

  it('pulls the token out of a scanned sign-in URL', () => {
    expect(extractInviteToken('http://192.168.1.9:8080/sign-in?token=lyc_abc123')).toBe(
      'lyc_abc123',
    )
  })

  it('url-decodes the token from the query', () => {
    expect(extractInviteToken('https://lib.example/sign-in?token=lyc_a%2Bb')).toBe('lyc_a+b')
  })

  it('rejects a URL with no token param', () => {
    expect(extractInviteToken('http://192.168.1.9:8080/sign-in')).toBeNull()
  })

  it('rejects a non-token string', () => {
    expect(extractInviteToken('hello there')).toBeNull()
  })

  it('rejects the bare prefix with nothing after it', () => {
    expect(extractInviteToken('lyc_')).toBeNull()
  })

  it('rejects empty / whitespace-only input', () => {
    expect(extractInviteToken('   ')).toBeNull()
  })
})

describe('inviteSignInUrl', () => {
  it('builds a redemption URL and encodes the token', () => {
    expect(inviteSignInUrl('http://192.168.1.9:8080', 'lyc_a+b')).toBe(
      'http://192.168.1.9:8080/sign-in?token=lyc_a%2Bb',
    )
  })

  it('does not double a trailing slash on the origin', () => {
    expect(inviteSignInUrl('http://host/', 'lyc_x')).toBe('http://host/sign-in?token=lyc_x')
  })
})
