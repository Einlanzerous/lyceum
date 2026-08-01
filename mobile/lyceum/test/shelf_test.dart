import 'package:flutter_test/flutter_test.dart';
import 'package:lyceum/api/models.dart';
import 'package:lyceum/features/library/shelf.dart';

Book _book({
  required int id,
  String? title,
  String author = 'Anon',
  double? progress,
  String? addedAt,
  String? series,
  double? seriesIndex,
  String? readAt,
  bool finished = false,
}) => Book(
  id: id,
  title: title ?? 'Book $id',
  author: author,
  coverUrl: '',
  progress: progress,
  addedAt: addedAt,
  series: series,
  seriesIndex: seriesIndex,
  readAt: readAt,
  finished: finished,
);

void main() {
  group('memberStatus', () {
    test('classifies by progress', () {
      expect(memberStatus(_book(id: 1)), MemberStatus.notStarted);
      expect(
        memberStatus(_book(id: 2, progress: 0.4)),
        MemberStatus.inProgress,
      );
      expect(memberStatus(_book(id: 3, progress: 1)), MemberStatus.finished);
    });
  });

  group('finished flag', () {
    test('marks the book finished regardless of progress', () {
      final b = Book(
        id: 1,
        title: 'x',
        author: 'y',
        coverUrl: '',
        progress: 0.3,
        finished: true,
      );
      expect(memberStatus(b), MemberStatus.finished);
    });

    test('counts as 100% in the series aggregate', () {
      final books = [
        Book(
          id: 1,
          title: 'a',
          author: 'y',
          coverUrl: '',
          series: 'S',
          seriesIndex: 1,
          finished: true,
          progress: 0.3,
        ),
        _book(id: 2, series: 'S', seriesIndex: 2),
      ];
      final series = buildShelf(
        books,
        const SortState(key: SortKey.title, ascending: true),
      ).whereType<SeriesItem>().single.series;
      expect(series.progress, closeTo(0.5, 1e-9));
    });
  });

  group('resumeIndex', () {
    test('picks the furthest in-progress volume', () {
      final members = [
        _book(id: 1, progress: 1),
        _book(id: 2, progress: 0.7),
        _book(id: 3),
      ];
      expect(resumeIndex(members), 1);
    });

    test('falls back to first unstarted, then first', () {
      expect(resumeIndex([_book(id: 1, progress: 1), _book(id: 2)]), 1);
      expect(
        resumeIndex([_book(id: 1, progress: 1), _book(id: 2, progress: 1)]),
        0,
      );
    });
  });

  group('sortBooks', () {
    final books = [
      _book(
        id: 1,
        title: 'Mango',
        author: 'Clarke',
        addedAt: '2026-01-02T00:00:00Z',
      ),
      _book(
        id: 2,
        title: 'apple',
        author: 'Adams',
        addedAt: '2026-03-01T00:00:00Z',
      ),
      _book(
        id: 3,
        title: 'Zebra',
        author: 'Zola',
        addedAt: '2026-02-01T00:00:00Z',
      ),
    ];

    test('by title, case-insensitive', () {
      final asc = sortBooks(
        books,
        const SortState(key: SortKey.title, ascending: true),
      );
      expect(asc.map((b) => b.id), [2, 1, 3]);
    });

    test('by recently added (desc) using added_at', () {
      final desc = sortBooks(
        books,
        const SortState(key: SortKey.added, ascending: false),
      );
      expect(desc.map((b) => b.id), [2, 3, 1]);
    });

    test('does not mutate input', () {
      final input = [...books];
      sortBooks(input, const SortState(key: SortKey.title, ascending: true));
      expect(input.map((b) => b.id), [1, 2, 3]);
    });
  });

  group('buildShelf', () {
    test('rolls a ≥2 series into one item, keeps singletons loose', () {
      final books = [
        _book(
          id: 1,
          title: 'Annihilation',
          series: 'Southern Reach',
          seriesIndex: 1,
        ),
        _book(
          id: 2,
          title: 'Authority',
          series: 'Southern Reach',
          seriesIndex: 2,
        ),
        _book(id: 3, title: 'Dune'),
        _book(id: 4, title: 'Solo', series: 'Lonely', seriesIndex: 1),
      ];
      final items = buildShelf(
        books,
        const SortState(key: SortKey.title, ascending: true),
      );
      final seriesItems = items.whereType<SeriesItem>().toList();
      expect(seriesItems, hasLength(1));
      expect(seriesItems.first.series.name, 'Southern Reach');
      expect(seriesItems.first.series.members, hasLength(2));
      expect(items.whereType<BookItem>(), hasLength(2));
    });

    test('orders members by series index and averages progress', () {
      final books = [
        _book(id: 1, series: 'S', seriesIndex: 2, progress: 0.5),
        _book(id: 2, series: 'S', seriesIndex: 1, progress: 1),
        _book(id: 3, series: 'S', seriesIndex: 3),
      ];
      final items = buildShelf(
        books,
        const SortState(key: SortKey.title, ascending: true),
      );
      final series = items.whereType<SeriesItem>().single.series;
      expect(series.members.map((m) => m.id), [2, 1, 3]);
      expect(series.progress, closeTo(0.5, 1e-9));
    });

    test('groups case-insensitively', () {
      final books = [
        _book(id: 1, series: 'The Expanse', seriesIndex: 1),
        _book(id: 2, series: 'the expanse', seriesIndex: 2),
      ];
      final items = buildShelf(
        books,
        const SortState(key: SortKey.title, ascending: true),
      );
      expect(items, hasLength(1));
      expect(items.first, isA<SeriesItem>());
    });
  });

  group('pinnedBookId', () {
    test('returns the most-recently-read in-progress book', () {
      final books = [
        _book(id: 1, progress: 0.3, addedAt: null),
        Book(
          id: 1,
          title: 'A',
          author: 'x',
          coverUrl: '',
          progress: 0.3,
          readAt: '2026-01-01T00:00:00Z',
        ),
        Book(
          id: 2,
          title: 'B',
          author: 'x',
          coverUrl: '',
          progress: 0.6,
          readAt: '2026-05-01T00:00:00Z',
        ),
        Book(
          id: 3,
          title: 'C',
          author: 'x',
          coverUrl: '',
          progress: 1,
          readAt: '2026-06-01T00:00:00Z',
        ),
        _book(id: 4),
      ];
      expect(pinnedBookId(books), 2);
    });

    test('returns null when nothing is mid-read', () {
      expect(pinnedBookId([_book(id: 1)]), isNull);
    });

    // LYCM-108: finishing a volume used to drop its series out of the pinned
    // slot — the exact moment you most want the next book one tap away.
    test(
      'after finishing a volume, points at the next one and keeps the series pinned',
      () {
        final books = [
          // Sorts first by title, so the pin has to actually move the series tile.
          _book(id: 9, title: 'Aardvarks'),
          _book(
            id: 1,
            title: 'Chronicles 1',
            series: 'Chronicles',
            seriesIndex: 1,
            finished: true,
            readAt: '2026-01-01T00:00:00Z',
          ),
          _book(
            id: 2,
            title: 'Chronicles 2',
            series: 'Chronicles',
            seriesIndex: 2,
            finished: true,
            readAt: '2026-06-01T00:00:00Z',
          ),
          _book(
            id: 3,
            title: 'Chronicles 3',
            series: 'Chronicles',
            seriesIndex: 3,
          ),
        ];
        // The unread volume, not the one just closed — this id drives the grid
        // pin, the list pin and the Continue chip alike.
        expect(pinnedBookId(books), 3);

        final items = buildShelf(
          books,
          const SortState(key: SortKey.title, ascending: true),
          pinBookId: pinnedBookId(books),
        );
        expect(items.length, 2); // the loose book + the series tile
        expect(items.first, isA<SeriesItem>());
        expect((items.first as SeriesItem).series.resumeBook.id, 3);
      },
    );

    test('stops pinning a series once every volume is read', () {
      final books = [
        _book(
          id: 1,
          series: 'Done',
          seriesIndex: 1,
          finished: true,
          readAt: '2026-01-01T00:00:00Z',
        ),
        _book(
          id: 2,
          series: 'Done',
          seriesIndex: 2,
          finished: true,
          readAt: '2026-06-01T00:00:00Z',
        ),
      ];
      expect(pinnedBookId(books), isNull);
    });

    // A finished standalone read more recently must not clear the slot:
    // whatever else you are mid-way through is still the answer.
    test('falls through a dead-end candidate to the next most recent', () {
      final books = [
        _book(id: 1, progress: 0.4, readAt: '2026-05-01T00:00:00Z'),
        _book(id: 2, finished: true, readAt: '2026-06-01T00:00:00Z'),
      ];
      expect(pinnedBookId(books), 1);
    });

    // readAt is stamped from any saved position, including the progress=0 one a
    // still-open reader flushes before pagination settles. Opening a book and
    // closing it must not take the pin off what you are actually reading.
    test('ignores a book that was opened but never got anywhere', () {
      final books = [
        _book(id: 1, progress: 0.4, readAt: '2026-05-01T00:00:00Z'),
        _book(id: 2, progress: 0, readAt: '2026-06-01T00:00:00Z'),
      ];
      expect(pinnedBookId(books), 1);
    });

    // resumeIndex picks the furthest in-progress volume, which is not
    // necessarily the one you last had open. While a volume is unfinished, the
    // pin stays on it.
    test('keeps the pin on the volume you are part-way through', () {
      final books = [
        _book(
          id: 1,
          series: 'Split',
          seriesIndex: 1,
          progress: 0.2,
          readAt: '2026-06-01T00:00:00Z',
        ),
        _book(
          id: 3,
          series: 'Split',
          seriesIndex: 3,
          progress: 0.5,
          readAt: '2026-01-01T00:00:00Z',
        ),
      ];
      expect(pinnedBookId(books), 1);
    });

    // A one-book "series" renders as a loose book, so it is judged as one.
    test('treats a series of one as a standalone', () {
      final books = [
        _book(
          id: 1,
          series: 'Solo',
          seriesIndex: 1,
          finished: true,
          readAt: '2026-06-01T00:00:00Z',
        ),
      ];
      expect(pinnedBookId(books), isNull);
    });
  });

  group('pin to front', () {
    test('floats the series item holding the current read', () {
      final books = [
        _book(id: 1, title: 'Apple'),
        _book(id: 2, title: 'Boxed 1', series: 'Boxed', seriesIndex: 1),
        Book(
          id: 3,
          title: 'Boxed 2',
          author: 'x',
          coverUrl: '',
          series: 'Boxed',
          seriesIndex: 2,
          progress: 0.5,
          readAt: '2026-05-01T00:00:00Z',
        ),
      ];
      final items = buildShelf(
        books,
        const SortState(key: SortKey.title, ascending: true),
        pinBookId: 3,
      );
      expect(items.first, isA<SeriesItem>());
      expect((items.first as SeriesItem).series.name, 'Boxed');
    });

    test('floats a loose current-read book in the list', () {
      final books = [
        _book(id: 1, title: 'Apple'),
        _book(id: 2, title: 'Zebra'),
      ];
      final list = sortBooks(
        books,
        const SortState(key: SortKey.title, ascending: true),
        pinBookId: 2,
      );
      expect(list.first.id, 2);
    });
  });

  group('matchesQuery', () {
    final b = _book(
      id: 1,
      title: 'Piranesi',
      author: 'Clarke',
      series: 'Standalone',
    );
    test('matches title, author, series; empty matches all', () {
      expect(matchesQuery(b, 'pira'), isTrue);
      expect(matchesQuery(b, 'clar'), isTrue);
      expect(matchesQuery(b, 'standal'), isTrue);
      expect(matchesQuery(b, ''), isTrue);
      expect(matchesQuery(b, 'zzz'), isFalse);
    });
  });
}
