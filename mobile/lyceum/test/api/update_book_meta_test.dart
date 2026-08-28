import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:lyceum/api/client.dart';
import 'package:lyceum/features/review/series_field.dart';

void main() {
  late http.Request seen;
  LyceumClient client() => LyceumClient(
    baseUrl: 'http://lib.test',
    deviceId: 'pixel-9',
    httpClient: MockClient((req) async {
      seen = req;
      return http.Response(
        jsonEncode({'id': 12, 'title': 'T', 'author': 'A'}),
        200,
        headers: {'content-type': 'application/json'},
      );
    }),
  );

  test(
    'updateBookMeta sends only title/author when no series is given',
    () async {
      await client().updateBookMeta(12, 'T', 'A');
      expect(seen.method, 'PATCH');
      expect(seen.url.path, '/books/12');
      expect(jsonDecode(seen.body), {'title': 'T', 'author': 'A'});
    },
  );

  test('updateBookMeta carries the series and its number (LYCM-129)', () async {
    await client().updateBookMeta(
      12,
      'T',
      'A',
      series: 'Mistborn',
      seriesIndex: 2,
    );
    expect(jsonDecode(seen.body), {
      'title': 'T',
      'author': 'A',
      'series': 'Mistborn',
      'series_index': 2,
    });
  });

  test('an empty series clears it, with no position', () async {
    await client().updateBookMeta(12, 'T', 'A', series: '', seriesIndex: 4);
    expect(jsonDecode(seen.body), {
      'title': 'T',
      'author': 'A',
      'series': '',
      'series_index': 4,
    });
  });

  test('seriesIndexText renders a stored position as typed text', () {
    expect(seriesIndexText(4), '4');
    expect(seriesIndexText(3.5), '3.5');
    expect(seriesIndexText(null), '');
  });

  test('parseSeriesIndex reads what the field says', () {
    expect(parseSeriesIndex('3'), 3);
    expect(parseSeriesIndex(' 3.5 '), 3.5);
    expect(parseSeriesIndex(''), 0);
    expect(parseSeriesIndex('abc'), 0);
    expect(parseSeriesIndex('-1'), 0);
  });
}
