import { describe, expect, it } from 'vitest'
import { openEntries, rankWanted, suggestWanted } from './wantedMatch'
import type { Book } from '@/api/types'
import type { InventoryEntry } from '@/api/client'

const held: Book = {
  id: 2,
  title: 'Mistborn - The Final Empire',
  author: 'Sanderson, Brandon',
  cover_url: '',
  review_state: 'pending',
  review_flags: ['no_isbn'],
}

const finalEmpire: InventoryEntry = {
  id: 7,
  isbn: '9780765311788',
  title: 'The Final Empire',
  author: 'Brandon Sanderson',
  state: 'wanted',
}
const wellOfAscension: InventoryEntry = {
  id: 8,
  isbn: '9780765316882',
  title: 'The Well of Ascension',
  author: 'Brandon Sanderson',
  state: 'wanted',
}
const dune: InventoryEntry = {
  id: 9,
  isbn: '9780441172719',
  title: 'Dune',
  author: 'Frank Herbert',
  state: 'wanted',
}
const ingested: InventoryEntry = {
  id: 10,
  isbn: '9780000000010',
  title: 'Elantris',
  author: 'Brandon Sanderson',
  state: 'ingested',
  book_id: 5,
}
const decoy: InventoryEntry = {
  id: 11,
  isbn: '9780000000011',
  title: 'The Final Empire',
  author: 'Somebody Else',
  state: 'owned',
}

describe('openEntries', () => {
  it('keeps only entries still waiting for a book', () => {
    expect(
      openEntries([finalEmpire, ingested, decoy, { ...dune, book_id: 3 }]).map((r) => r.id),
    ).toEqual([7, 11])
  })
})

describe('rankWanted', () => {
  it('puts the same author first, best title match on top', () => {
    const ranked = rankWanted(held, [dune, decoy, wellOfAscension, finalEmpire])
    expect(ranked.map((r) => r.id)).toEqual([7, 8, 11, 9])
  })
})

describe('suggestWanted', () => {
  it('suggests the same-author entry whose title the held title contains', () => {
    // "Mistborn - The Final Empire" (a packager's series prefix) vs "The Final
    // Empire"; the author is inverted in the EPUB.
    expect(suggestWanted(held, [dune, decoy, wellOfAscension, finalEmpire])?.id).toBe(7)
  })

  it('matches across case, accents, punctuation and a leading article', () => {
    const book: Book = {
      id: 1,
      title: 'harry potter and the philosopher’s stone',
      author: 'J. K. Rowling',
      cover_url: '',
    }
    const row: InventoryEntry = {
      id: 1,
      isbn: 'x',
      title: "Harry Potter and the Philosopher's Stone",
      author: 'Rowling, J.K.',
      state: 'wanted',
    }
    expect(suggestWanted(book, [row])?.id).toBe(1)
  })

  it('does not preselect a same-author entry whose title is merely a stem of the book’s', () => {
    // "Dune Messiah" contains "Dune"; both by Herbert. Linking would put Messiah
    // on Dune's wanted row and stop Dune ever being grabbed. Offered, ranked
    // first, but not selected.
    const messiah: Book = { id: 3, title: 'Dune Messiah', author: 'Frank Herbert', cover_url: '' }
    expect(rankWanted(messiah, [wellOfAscension, dune])[0]!.id).toBe(9)
    expect(suggestWanted(messiah, [wellOfAscension, dune])).toBeNull()
    // And the other way round: a longer entry title is not the book either.
    const dunePart: Book = { id: 4, title: 'Dune', author: 'Frank Herbert', cover_url: '' }
    const messiahRow: InventoryEntry = {
      id: 12,
      isbn: 'y',
      title: 'Dune Messiah',
      author: 'Frank Herbert',
      state: 'wanted',
    }
    expect(suggestWanted(dunePart, [messiahRow])).toBeNull()
  })

  it('undoes the "Hobbit, The" inversion like the server does', () => {
    const book: Book = { id: 5, title: 'The Hobbit', author: 'J. R. R. Tolkien', cover_url: '' }
    const row: InventoryEntry = {
      id: 13,
      isbn: 'z',
      title: 'Hobbit, The',
      author: 'Tolkien, J. R. R.',
      state: 'wanted',
    }
    expect(suggestWanted(book, [row])?.id).toBe(13)
  })

  it('does not suggest a different author even with the same title', () => {
    expect(suggestWanted(held, [decoy, dune])).toBeNull()
  })

  it('does not suggest the same author with an unrelated title', () => {
    expect(suggestWanted(held, [wellOfAscension])).toBeNull()
  })

  it('has nothing to suggest for an empty list', () => {
    expect(suggestWanted(held, [])).toBeNull()
  })
})
