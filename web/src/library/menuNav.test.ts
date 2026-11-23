import { afterEach, describe, expect, it } from 'vitest'
import { menuItems, navigateMenu } from './menuNav'

function build(): HTMLElement[] {
  document.body.innerHTML =
    '<div id="m"><button>a</button><button>b</button><button>c</button></div>'
  return menuItems(document.getElementById('m'), 'button')
}
function key(k: string): KeyboardEvent {
  return new KeyboardEvent('keydown', { key: k, cancelable: true })
}

describe('navigateMenu', () => {
  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('moves focus down and up, wrapping at both ends', () => {
    const items = build()
    items[0]!.focus()
    navigateMenu(key('ArrowDown'), items)
    expect(document.activeElement).toBe(items[1])
    navigateMenu(key('ArrowDown'), items)
    navigateMenu(key('ArrowDown'), items)
    expect(document.activeElement).toBe(items[0])
    navigateMenu(key('ArrowUp'), items)
    expect(document.activeElement).toBe(items[2])
  })

  it('jumps with Home and End and enters from outside the menu', () => {
    const items = build()
    navigateMenu(key('End'), items)
    expect(document.activeElement).toBe(items[2])
    navigateMenu(key('Home'), items)
    expect(document.activeElement).toBe(items[0])
    ;(document.activeElement as HTMLElement).blur()
    navigateMenu(key('ArrowUp'), items)
    expect(document.activeElement).toBe(items[2])
  })

  it('leaves other keys alone', () => {
    const items = build()
    const e = key('a')
    expect(navigateMenu(e, items)).toBe(false)
    expect(e.defaultPrevented).toBe(false)
  })
})
