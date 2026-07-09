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
}) => Book(
  id: id,
  title: title ?? 'Book $id',
  author: author,
  coverUrl: '',
  progress: progress,
  addedAt: addedAt,
  series: series,
  seriesIndex: seriesIndex,
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
