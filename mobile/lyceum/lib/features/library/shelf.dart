// Pure library-shelf logic: sorting (LYCM-62) and series roll-up (LYCM-36).
// Mirrors web/src/library/{sort,series}.ts so the two clients group and order
// the shelf identically. No Flutter imports here — kept unit-testable.
import '../../api/models.dart';

enum SortKey { added, title, author }

extension SortKeyLabel on SortKey {
  String get label => switch (this) {
    SortKey.added => 'Recently added',
    SortKey.title => 'Title',
    SortKey.author => 'Author',
  };

  String get storageValue => name;

  /// Newest-first for dates, A→Z for text.
  bool get defaultAscending => this != SortKey.added;
}

SortKey sortKeyFromStorage(String? value) => switch (value) {
  'title' => SortKey.title,
  'author' => SortKey.author,
  _ => SortKey.added,
};

class SortState {
  const SortState({this.key = SortKey.added, this.ascending = false});
  final SortKey key;
  final bool ascending;

  SortState copyWith({SortKey? key, bool? ascending}) =>
      SortState(key: key ?? this.key, ascending: ascending ?? this.ascending);
}

/// A book is "finished" once progress reaches (near) 1.
const double kFinishedAt = 0.99;

enum MemberStatus { finished, inProgress, notStarted }

MemberStatus memberStatus(Book b) {
  final p = b.progress ?? 0;
  if (p >= kFinishedAt) return MemberStatus.finished;
  if (p > 0) return MemberStatus.inProgress;
  return MemberStatus.notStarted;
}

extension MemberStatusLabel on MemberStatus {
  String get label => switch (this) {
    MemberStatus.finished => 'Finished',
    MemberStatus.inProgress => 'In progress',
    MemberStatus.notStarted => 'Not started',
  };
}

/// A series of ≥2 books rolled up for display.
class SeriesGroup {
  SeriesGroup({
    required this.name,
    required this.author,
    required this.members,
    required this.progress,
    required this.coverBook,
  });

  final String name;
  final String author;

  /// Members in reading order (by series_index, then title).
  final List<Book> members;

  /// Aggregate progress 0..1 — the mean of member progress.
  final double progress;

  /// The book whose cover represents the stack.
  final Book coverBook;
}

/// A shelf entry: either a loose book or a rolled-up series.
sealed class ShelfItem {
  const ShelfItem();
}

class BookItem extends ShelfItem {
  const BookItem(this.book);
  final Book book;
}

class SeriesItem extends ShelfItem {
  const SeriesItem(this.series);
  final SeriesGroup series;
}

/// The volume "Resume" should open: the furthest in-progress volume, else the
/// first unstarted, else — everything read — the first.
int resumeIndex(List<Book> members) {
  var lastInProgress = -1;
  var firstUnstarted = -1;
  for (var i = 0; i < members.length; i++) {
    switch (memberStatus(members[i])) {
      case MemberStatus.inProgress:
        lastInProgress = i;
      case MemberStatus.notStarted:
        if (firstUnstarted == -1) firstUnstarted = i;
      case MemberStatus.finished:
        break;
    }
  }
  if (lastInProgress != -1) return lastInProgress;
  if (firstUnstarted != -1) return firstUnstarted;
  return 0;
}

/// The id of the book to pin to the top of the shelf: the most-recently-read
/// book still in progress (your "continue reading"), or null when none is
/// mid-read.
int? pinnedBookId(List<Book> books) {
  Book? best;
  for (final b in books) {
    if (b.readAt == null) continue;
    final p = b.progress ?? 0;
    if (p <= 0 || p >= kFinishedAt) continue;
    if (best == null || b.readAt!.compareTo(best.readAt ?? '') > 0) best = b;
  }
  return best?.id;
}

int _compareText(String a, String b) =>
    a.toLowerCase().compareTo(b.toLowerCase());

int _compareBooks(SortKey key, Book a, Book b) {
  switch (key) {
    case SortKey.title:
      final t = _compareText(a.title, b.title);
      return t != 0 ? t : a.id - b.id;
    case SortKey.author:
      if (a.author.isEmpty != b.author.isEmpty) {
        return a.author.isEmpty ? 1 : -1;
      }
      final au = _compareText(a.author, b.author);
      if (au != 0) return au;
      final t = _compareText(a.title, b.title);
      return t != 0 ? t : a.id - b.id;
    case SortKey.added:
      // added_at is RFC3339 UTC, so lexical compare is chronological.
      final ad = (a.addedAt ?? '').compareTo(b.addedAt ?? '');
      return ad != 0 ? ad : a.id - b.id;
  }
}

/// Sort a flat list of books (used for search results and the list view). When
/// [pinBookId] is given, that book floats to the front after sorting.
List<Book> sortBooks(List<Book> books, SortState sort, {int? pinBookId}) {
  final out = [...books]..sort((a, b) => _compareBooks(sort.key, a, b));
  final ordered = sort.ascending ? out : out.reversed.toList();
  if (pinBookId != null) {
    final at = ordered.indexWhere((b) => b.id == pinBookId);
    if (at > 0) ordered.insert(0, ordered.removeAt(at));
  }
  return ordered;
}

String _newestAdded(List<Book> books) => books.fold(
  '',
  (max, b) => (b.addedAt ?? '').compareTo(max) > 0 ? (b.addedAt ?? '') : max,
);

String _pickAuthor(List<Book> members) {
  final counts = <String, int>{};
  for (final m in members) {
    final a = m.author.trim();
    if (a.isNotEmpty) counts[a] = (counts[a] ?? 0) + 1;
  }
  var best = '';
  var bestN = 0;
  counts.forEach((a, n) {
    if (n > bestN) {
      best = a;
      bestN = n;
    }
  });
  return best.isNotEmpty
      ? best
      : (members.isNotEmpty ? members.first.author : '');
}

SeriesGroup _buildGroup(String name, List<Book> members) {
  final ordered = [...members]
    ..sort((a, b) {
      final ai = a.seriesIndex ?? double.infinity;
      final bi = b.seriesIndex ?? double.infinity;
      if (ai != bi) return ai.compareTo(bi);
      final t = _compareText(a.title, b.title);
      return t != 0 ? t : a.id - b.id;
    });
  final progress =
      ordered.fold<double>(0, (s, m) => s + (m.progress ?? 0)) / ordered.length;
  final coverBook = ordered.firstWhere(
    (m) => m.hasCover,
    orElse: () => ordered.first,
  );
  return SeriesGroup(
    name: name,
    author: _pickAuthor(ordered),
    members: ordered,
    progress: progress,
    coverBook: coverBook,
  );
}

/// Group books into shelf items and order them by [sort]. A series of ≥2 books
/// becomes one series item; a series of 1 (or none) stays a loose book.
List<ShelfItem> buildShelf(List<Book> books, SortState sort, {int? pinBookId}) {
  final groups = <String, ({String name, List<Book> members})>{};
  final loose = <Book>[];

  for (final b in books) {
    final series = (b.series ?? '').trim();
    if (series.isEmpty) {
      loose.add(b);
      continue;
    }
    final key = series.toLowerCase();
    final g = groups[key];
    if (g != null) {
      g.members.add(b);
    } else {
      groups[key] = (name: series, members: [b]);
    }
  }

  final entries =
      <({ShelfItem item, String title, String author, String added, int id})>[];
  for (final b in loose) {
    entries.add((
      item: BookItem(b),
      title: b.title,
      author: b.author,
      added: b.addedAt ?? '',
      id: b.id,
    ));
  }
  for (final g in groups.values) {
    if (g.members.length == 1) {
      final only = g.members.first;
      entries.add((
        item: BookItem(only),
        title: only.title,
        author: only.author,
        added: only.addedAt ?? '',
        id: only.id,
      ));
    } else {
      final series = _buildGroup(g.name, g.members);
      entries.add((
        item: SeriesItem(series),
        title: series.name,
        author: series.author,
        added: _newestAdded(series.members),
        id: series.members.map((m) => m.id).reduce((a, b) => a < b ? a : b),
      ));
    }
  }

  int cmp(
    ({ShelfItem item, String title, String author, String added, int id}) a,
    ({ShelfItem item, String title, String author, String added, int id}) b,
  ) {
    switch (sort.key) {
      case SortKey.title:
        final t = _compareText(a.title, b.title);
        return t != 0 ? t : a.id - b.id;
      case SortKey.author:
        if (a.author.isEmpty != b.author.isEmpty) {
          return a.author.isEmpty ? 1 : -1;
        }
        final au = _compareText(a.author, b.author);
        if (au != 0) return au;
        final t = _compareText(a.title, b.title);
        return t != 0 ? t : a.id - b.id;
      case SortKey.added:
        final ad = a.added.compareTo(b.added);
        return ad != 0 ? ad : a.id - b.id;
    }
  }

  entries.sort(cmp);
  final ordered = (sort.ascending ? entries : entries.reversed)
      .map((e) => e.item)
      .toList();

  // Pin the shelf item holding the current read to the front — the book if it's
  // loose, or its series item if it belongs to one (keeping the group intact).
  if (pinBookId != null) {
    final at = ordered.indexWhere(
      (item) => switch (item) {
        BookItem(:final book) => book.id == pinBookId,
        SeriesItem(:final series) => series.members.any(
          (m) => m.id == pinBookId,
        ),
      },
    );
    if (at > 0) ordered.insert(0, ordered.removeAt(at));
  }
  return ordered;
}

/// Case-insensitive title/author/series match for the search overlay.
bool matchesQuery(Book b, String query) {
  final q = query.trim().toLowerCase();
  if (q.isEmpty) return true;
  return b.title.toLowerCase().contains(q) ||
      b.author.toLowerCase().contains(q) ||
      (b.series ?? '').toLowerCase().contains(q);
}
