import { describe, expect, it } from 'vitest'
import { formatBytes, getInitials, getSenderAddress, getSenderName } from './format'

describe('getSenderName', () => {
  it('prefers the display name', () => {
    expect(getSenderName({ name: ' Jane ', address: 'j@example.com' })).toBe('Jane')
  })
  it('falls back to the address, then a placeholder', () => {
    expect(getSenderName({ name: '', address: 'j@example.com' })).toBe('j@example.com')
    expect(getSenderName(null)).toBe('Unknown sender')
  })
  it('accepts a bare string', () => {
    expect(getSenderName('someone@example.com')).toBe('someone@example.com')
  })
})

describe('getSenderAddress', () => {
  it('returns the address or an empty string', () => {
    expect(getSenderAddress({ name: 'R', address: 'j@example.com' })).toBe('j@example.com')
    expect(getSenderAddress(undefined)).toBe('')
  })
})

describe('getInitials', () => {
  it('uses the unique suffix of generated emails', () => {
    expect(getInitials('old_prefix_efdd2y46@mailtm.example')).toBe('EF')
    expect(getInitials('old_prefix_z3nj6tzo@mailtm.example')).toBe('Z3')
  })
  it('uses first letters of a two-word name', () => {
    expect(getInitials('jane doe')).toBe('JD')
  })
  it('handles single words and empty input', () => {
    expect(getInitials('william')).toBe('WI')
    expect(getInitials('')).toBe('?')
  })
})

describe('formatBytes', () => {
  it.each([
    [0, '0 B'],
    [512, '512 B'],
    [1536, '1.5 KB'],
    [20 * 1024, '20 KB'],
    [3.2 * 1024 * 1024, '3.2 MB'],
    [-1, '-'],
  ])('%s -> %s', (bytes, label) => {
    expect(formatBytes(bytes)).toBe(label)
  })
})
