import 'package:flutter_test/flutter_test.dart';
import 'package:lyceum/features/scan/isbn.dart';

void main() {
  group('normalizeIsbn', () {
    test('accepts a valid ISBN-13 (EAN-13) as-is', () {
      expect(normalizeIsbn('9780140449334'), '9780140449334');
    });

    test('strips hyphens, spaces, and an "urn:isbn:" prefix', () {
      expect(normalizeIsbn('978-0-14-044933-4'), '9780140449334');
      expect(normalizeIsbn('urn:isbn:9780140449334'), '9780140449334');
      expect(normalizeIsbn('  ISBN 978 0 14 044933 4 '), '9780140449334');
    });

    test('converts a valid ISBN-10 to ISBN-13', () {
      // 0-14-044933-6 is the ISBN-10 for the same edition.
      expect(normalizeIsbn('0140449337'), '9780140449334');
    });

    test('accepts an ISBN-10 with an X check digit', () {
      // 043942089X — a well-known valid ISBN-10 ending in X.
      final got = normalizeIsbn('043942089X');
      expect(got, isNotNull);
      expect(got!.length, 13);
      expect(got.startsWith('978'), isTrue);
      // lower-case x is tolerated too.
      expect(normalizeIsbn('043942089x'), got);
    });

    test('rejects a bad ISBN-13 checksum', () {
      expect(normalizeIsbn('9780140449335'), isNull);
    });

    test('rejects a bad ISBN-10 checksum', () {
      expect(normalizeIsbn('0140449338'), isNull);
    });

    test('rejects a non-book EAN-13 (bad checksum / not Bookland)', () {
      // A random 13-digit product code that fails the checksum.
      expect(normalizeIsbn('1234567890123'), isNull);
    });

    test('rejects a valid-checksum EAN-13 outside the Bookland range', () {
      // 5901234567893 is a well-formed EAN-13 (valid checksum) but a product
      // barcode, not an ISBN — the price/product code that shares a back cover
      // with the ISBN must not be accepted as a book (LYCM-75).
      expect(normalizeIsbn('5901234567893'), isNull);
    });

    test('accepts a 979-prefixed ISBN-13', () {
      // 9791234567896 — Bookland 979 range, valid EAN-13 checksum.
      expect(normalizeIsbn('9791234567896'), '9791234567896');
    });

    test('rejects junk and wrong-length input', () {
      expect(normalizeIsbn(''), isNull);
      expect(normalizeIsbn('not an isbn'), isNull);
      expect(normalizeIsbn('12345'), isNull);
      expect(normalizeIsbn('97801404493349'), isNull); // 14 digits
    });
  });

  group('isValidIsbn', () {
    test('agrees with normalizeIsbn', () {
      expect(isValidIsbn('9780140449334'), isTrue);
      expect(isValidIsbn('0140449337'), isTrue);
      expect(isValidIsbn('9780140449335'), isFalse);
      expect(isValidIsbn('nope'), isFalse);
    });
  });
}
