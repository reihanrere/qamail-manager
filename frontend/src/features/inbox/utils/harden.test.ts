import { describe, expect, it } from 'vitest'
import { hardenEmailLinks } from './harden'

const SAFE = 'rel="noopener noreferrer"'

describe('hardenEmailLinks', () => {
  it('adds noopener noreferrer to plain links', () => {
    expect(hardenEmailLinks('<a href="https://x.test">go</a>')).toBe(`<a href="https://x.test" ${SAFE}>go</a>`)
  })

  it.each([
    ['double quotes', '<a href="https://x.test" rel="opener">go</a>'],
    ['single quotes', "<a href='https://x.test' rel='opener'>go</a>"],
    ['unquoted', '<a href=https://x.test rel=opener>go</a>'],
    ['upper case', '<A HREF="https://x.test" REL="opener">go</A>'],
    ['spaces around equals', '<a href="https://x.test" rel = "opener">go</a>'],
    ['rel first', '<a rel="opener" href="https://x.test">go</a>'],
    ['slash as separator', '<a/rel="opener" href="https://x.test">go</a>'],
    ['newline before rel', '<a href="https://x.test"\n\trel="opener">go</a>'],
  ])('replaces an existing rel (%s)', (_name, html) => {
    const out = hardenEmailLinks(html)
    expect(out).not.toMatch(/rel\s*=\s*["']?opener/i)
    expect(out).toContain(SAFE)
    expect(out.match(/rel\s*=/gi)).toHaveLength(1)
  })

  it('removes every duplicate rel, because the first one in the tag wins in a browser', () => {
    const out = hardenEmailLinks('<a rel="opener" href="https://x.test" rel="noopener">go</a>')
    expect(out.match(/rel\s*=/gi)).toHaveLength(1)
    expect(out).toContain(SAFE)
  })

  it('handles image maps and a ">" inside an attribute value', () => {
    expect(hardenEmailLinks('<area href="https://x.test" rel="opener">')).toContain(SAFE)
    const out = hardenEmailLinks('<a title="a > b" href="https://x.test" rel="opener">go</a>')
    expect(out).toContain('title="a > b"')
    expect(out).toContain(SAFE)
    expect(out).not.toMatch(/rel\s*=\s*"opener/)
  })

  it('leaves other tags and text alone', () => {
    const html = '<p>rel="opener" <abbr title="a">x</abbr> <img src="a.png" rel="opener"></p>'
    expect(hardenEmailLinks(html)).toBe(html)
  })
})
