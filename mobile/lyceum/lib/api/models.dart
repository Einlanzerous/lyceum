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

  bool get hasCover => coverUrl.isNotEmpty;

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
