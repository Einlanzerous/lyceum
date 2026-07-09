import 'package:flutter_test/flutter_test.dart';
import 'package:lyceum/api/models.dart';
import 'package:lyceum/api/server_store.dart';

void main() {
  group('normalizeServerUrl', () {
    test('trims and strips trailing slashes', () {
      expect(normalizeServerUrl('  http://host:8080/  '), 'http://host:8080');
      expect(normalizeServerUrl('http://host:8080///'), 'http://host:8080');
      expect(normalizeServerUrl('http://host:8080'), 'http://host:8080');
      expect(normalizeServerUrl('   '), '');
    });
  });

  group('Book.fromJson', () {
    test('parses a full row', () {
      final b = Book.fromJson({
        'id': 7,
        'title': 'Meditations',
        'author': 'Marcus Aurelius',
        'cover_url': '/books/7/cover',
        'progress': 0.42,
      });
      expect(b.id, 7);
      expect(b.title, 'Meditations');
      expect(b.hasCover, isTrue);
      expect(b.progress, closeTo(0.42, 1e-9));
    });

    test('handles missing cover + progress', () {
      final b = Book.fromJson({
        'id': 1,
        'title': 'Untitled',
        'author': '',
        'cover_url': '',
      });
      expect(b.hasCover, isFalse);
      expect(b.progress, isNull);
    });

    test('parses series metadata and added_at', () {
      final b = Book.fromJson({
        'id': 2,
        'title': 'Authority',
        'author': 'VanderMeer',
        'cover_url': '',
        'added_at': '2026-07-01T00:00:00Z',
        'series': 'Southern Reach',
        'series_index': 2,
      });
      expect(b.series, 'Southern Reach');
      expect(b.seriesIndex, closeTo(2, 1e-9));
      expect(b.addedAt, '2026-07-01T00:00:00Z');
    });

    test('series fields default to null when absent', () {
      final b = Book.fromJson({
        'id': 1,
        'title': 'Standalone',
        'author': '',
        'cover_url': '',
      });
      expect(b.series, isNull);
      expect(b.seriesIndex, isNull);
      expect(b.addedAt, isNull);
    });
  });

  group('Position round-trip', () {
    test('toJson matches the wire shape', () {
      const p = Position(
        bookId: 3,
        deviceId: 'abc',
        cfi: 'epubcfi(/6/4!/4/2)',
        progress: 0.5,
        updatedAt: '2026-07-01T00:00:00Z',
      );
      expect(p.toJson(), {
        'book_id': 3,
        'device_id': 'abc',
        'cfi': 'epubcfi(/6/4!/4/2)',
        'progress': 0.5,
        'updated_at': '2026-07-01T00:00:00Z',
      });
    });
  });
}
