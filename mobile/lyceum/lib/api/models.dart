// Wire models mirroring `web/src/api/types.ts` and the Go `*JSON` shapes.
// Hand-written (de)serialization — no build_runner, matching Argosy's
// lightweight approach.

class Book {
  const Book({
    required this.id,
    required this.title,
    required this.author,
    required this.coverUrl,
    this.progress,
    this.addedAt,
    this.readAt,
    this.series,
    this.seriesIndex,
    this.finished = false,
  });

  final int id;
  final String title;
  final String author;

  /// Relative cover path (`/books/{id}/cover`) or `""` when the book has no
  /// cover. Combine with the client's base URL for an absolute image URL.
  final String coverUrl;

  /// Reading progress in `0..1`, or null when the book was never opened.
  final double? progress;

  /// RFC3339 timestamp the book was ingested; backs the "recently added" sort.
  final String? addedAt;

  /// RFC3339 timestamp of the latest reading position; pins the current read.
  final String? readAt;

  /// Series the book belongs to, or null/"" when it is a standalone (LYCM-36).
  final String? series;

  /// 1-based position within [series], or null when unknown.
  final double? seriesIndex;

  /// True when the book has been explicitly marked read (independent of progress).
  final bool finished;

  bool get hasCover => coverUrl.isNotEmpty;

  Book copyWith({bool? finished}) => Book(
    id: id,
    title: title,
    author: author,
    coverUrl: coverUrl,
    progress: progress,
    addedAt: addedAt,
    readAt: readAt,
    series: series,
    seriesIndex: seriesIndex,
    finished: finished ?? this.finished,
  );

  factory Book.fromJson(Map<String, dynamic> json) => Book(
    id: (json['id'] as num).toInt(),
    title: (json['title'] as String?) ?? '',
    author: (json['author'] as String?) ?? '',
    coverUrl: (json['cover_url'] as String?) ?? '',
    progress: (json['progress'] as num?)?.toDouble(),
    addedAt: json['added_at'] as String?,
    readAt: json['read_at'] as String?,
    series: json['series'] as String?,
    seriesIndex: (json['series_index'] as num?)?.toDouble(),
    finished: (json['finished'] as bool?) ?? false,
  );
}

class Position {
  const Position({
    required this.bookId,
    required this.deviceId,
    required this.cfi,
    required this.progress,
    required this.updatedAt,
  });

  final int bookId;
  final String deviceId;
  final String cfi;
  final double progress;

  /// ISO-8601 timestamp; drives last-write-wins on the server.
  final String updatedAt;

  factory Position.fromJson(Map<String, dynamic> json) => Position(
    bookId: (json['book_id'] as num).toInt(),
    deviceId: (json['device_id'] as String?) ?? '',
    cfi: (json['cfi'] as String?) ?? '',
    progress: (json['progress'] as num?)?.toDouble() ?? 0,
    updatedAt: (json['updated_at'] as String?) ?? '',
  );

  Map<String, dynamic> toJson() => {
    'book_id': bookId,
    'device_id': deviceId,
    'cfi': cfi,
    'progress': progress,
    'updated_at': updatedAt,
  };
}

/// Physical-library inventory row (ISBN-keyed). Included for completeness;
/// the first release focuses on the digital shelf.
class InventoryItem {
  const InventoryItem({
    required this.id,
    required this.isbn,
    required this.state,
    this.title,
    this.author,
    this.bookId,
  });

  final int id;
  final String isbn;
  final String state; // owned | wanted | acquiring | ingested
  final String? title;
  final String? author;
  final int? bookId;

  factory InventoryItem.fromJson(Map<String, dynamic> json) => InventoryItem(
    id: (json['id'] as num).toInt(),
    isbn: (json['isbn'] as String?) ?? '',
    state: (json['state'] as String?) ?? 'owned',
    title: json['title'] as String?,
    author: json['author'] as String?,
    bookId: (json['book_id'] as num?)?.toInt(),
  );
}

/// One ISBN captured by the scanner, awaiting flush to a batch (LYCM-602). The
/// wire shape matches the Go `scanJSON`: `{isbn, captured_at, source}`.
class ScannedIsbn {
  const ScannedIsbn({
    required this.isbn,
    required this.capturedAt,
    this.source = 'camera',
  });

  /// Canonical ISBN-13 (already normalized by the scanner).
  final String isbn;

  /// When the scan happened; serialized as RFC3339 UTC.
  final DateTime capturedAt;

  /// How it was captured: `camera` (barcode) or `manual` (typed/pasted).
  final String source;

  Map<String, dynamic> toJson() => {
    'isbn': isbn,
    'captured_at': capturedAt.toUtc().toIso8601String(),
    'source': source,
  };

  factory ScannedIsbn.fromJson(Map<String, dynamic> json) => ScannedIsbn(
    isbn: (json['isbn'] as String?) ?? '',
    capturedAt:
        DateTime.tryParse((json['captured_at'] as String?) ?? '')?.toUtc() ??
        DateTime.fromMillisecondsSinceEpoch(0, isUtc: true),
    source: (json['source'] as String?) ?? 'camera',
  );
}

/// Per-status tally the server returns for a reviewed batch. The phone shows it
/// as a read-only summary; review/confirm happens on web/desktop.
class BatchCounts {
  const BatchCounts({
    this.ready = 0,
    this.review = 0,
    this.noMatch = 0,
    this.duplicate = 0,
    this.confirmed = 0,
    this.skipped = 0,
  });

  final int ready;
  final int review;
  final int noMatch;
  final int duplicate;
  final int confirmed;
  final int skipped;

  int get total => ready + review + noMatch + duplicate + confirmed + skipped;

  factory BatchCounts.fromJson(Map<String, dynamic>? json) {
    int at(String k) => (json?[k] as num?)?.toInt() ?? 0;
    return BatchCounts(
      ready: at('ready'),
      review: at('review'),
      noMatch: at('no_match'),
      duplicate: at('duplicate'),
      confirmed: at('confirmed'),
      skipped: at('skipped'),
    );
  }
}

/// One resolved scan in a batch. Only the fields the phone renders read-only.
class Candidate {
  const Candidate({
    required this.isbn,
    required this.status,
    this.title,
    this.author,
  });

  final String isbn;

  /// ready | review | no_match | duplicate | confirmed | skipped.
  final String status;
  final String? title;
  final String? author;

  factory Candidate.fromJson(Map<String, dynamic> json) => Candidate(
    isbn: (json['isbn'] as String?) ?? '',
    status: (json['status'] as String?) ?? '',
    title: json['title'] as String?,
    author: json['author'] as String?,
  );
}

/// A review batch created from a set of scans (LYCM-603). The phone creates one
/// and shows its result; the actual verify/confirm is a web/desktop step.
class Batch {
  const Batch({
    required this.id,
    required this.status,
    required this.counts,
    required this.candidates,
  });

  final int id;
  final String status;
  final BatchCounts counts;
  final List<Candidate> candidates;

  factory Batch.fromJson(Map<String, dynamic> json) => Batch(
    id: (json['id'] as num).toInt(),
    status: (json['status'] as String?) ?? '',
    counts: BatchCounts.fromJson(json['counts'] as Map<String, dynamic>?),
    candidates: ((json['candidates'] as List<dynamic>?) ?? const [])
        .map((e) => Candidate.fromJson(e as Map<String, dynamic>))
        .toList(growable: false),
  );
}
