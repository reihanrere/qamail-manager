// An opening <a> or <area> tag, allowing quoted attribute values that contain ">"
const LINK_TAG = /<(a|area)(?=[\s/>])((?:"[^"]*"|'[^']*'|[^'">])*)>/gi
// A rel attribute (value quoted or bare), with the whitespace or slash that precedes it
const REL_ATTRIBUTE = /[\s/]rel\s*=\s*(?:"[^"]*"|'[^']*'|[^\s>]*)/gi

/**
 * Forces rel="noopener noreferrer" on every link of an email body.
 *
 * The email is shown in an iframe whose sandbox lets popups escape it. Without noopener a
 * sender could keep a handle on the app through window.opener (rel="opener" overrides the
 * implicit noopener of target=_blank) and steer the app's tab to a fake login page.
 * Existing rel attributes are removed first: a browser honours the first one in the tag.
 */
export function hardenEmailLinks(html: string): string {
  return html.replace(LINK_TAG, (_match, name: string, attributes: string) => {
    const kept = attributes.replace(REL_ATTRIBUTE, '')
    return `<${name}${kept} rel="noopener noreferrer">`
  })
}
