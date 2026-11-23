// Roving keyboard navigation for the app's menus (LYCM-142): AccountMenu,
// SortControl and TileMenu declare menu/listbox roles, and those roles promise
// arrow-key movement. Items are `tabindex="-1"` and focused programmatically, so
// the menu is one Tab stop (or none: Tab closes it) rather than N.

/**
 * Handle ArrowDown / ArrowUp (wrapping), Home and End over `items`. Returns true
 * when the key was a navigation key, so the caller can stop it scrolling the page.
 */
export function navigateMenu(e: KeyboardEvent, items: readonly HTMLElement[]): boolean {
  if (items.length === 0) return false
  const at = items.findIndex((el) => el === document.activeElement)
  let next: number
  switch (e.key) {
    case 'ArrowDown':
      next = at < 0 ? 0 : (at + 1) % items.length
      break
    case 'ArrowUp':
      next = at < 0 ? items.length - 1 : (at - 1 + items.length) % items.length
      break
    case 'Home':
      next = 0
      break
    case 'End':
      next = items.length - 1
      break
    default:
      return false
  }
  e.preventDefault()
  items[next]?.focus()
  return true
}

/** The focusable items of a menu element, in DOM order. */
export function menuItems(menu: ParentNode | null, selector: string): HTMLElement[] {
  return menu ? Array.from(menu.querySelectorAll<HTMLElement>(selector)) : []
}
