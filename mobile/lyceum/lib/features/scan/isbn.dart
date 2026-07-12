/// Pure ISBN handling for the barcode scanner (LYCM-602) — no Flutter imports so
/// it is unit-testable without a camera. Mirrors the Go backend `internal/isbn`
/// (checksum validation + ISBN-10→13 conversion) so the phone and server agree
/// on what a "valid ISBN" is.
library;

/// Normalizes raw scanner/manual input to a canonical ISBN-13, or returns null
/// when it is not a valid ISBN-10 or ISBN-13.
///
/// It tolerates hyphens, spaces, and an "ISBN"/"urn:isbn:" prefix (everything
/// non-significant is stripped), accepts a valid ISBN-10 (converting it to 13),
/// and validates the EAN-13 checksum. A back-cover book barcode is an EAN-13 in
/// the 978/979 Bookland range, so a scanned EAN-13 that passes the checksum is
/// taken as the ISBN-13 directly.
String? normalizeIsbn(String raw) {
  final s = _clean(raw);
  switch (s.length) {
    case 10:
      if (_valid10(s)) return _to13(s);
    case 13:
      if (_valid13(s)) return s;
  }
  return null;
}

/// Whether [raw] normalizes to a valid ISBN.
bool isValidIsbn(String raw) => normalizeIsbn(raw) != null;

/// Keeps only ISBN-significant characters: digits and an 'X'/'x' ISBN-10 check
/// digit (uppercased). Hyphens, spaces, and "urn:isbn:" fall away. A non-ISBN
/// value rarely cleans to a 10/13-length string that also passes the checksum,
/// so the length+checksum gate in [normalizeIsbn] rejects it.
String _clean(String raw) {
  final b = StringBuffer();
  for (final r in raw.codeUnits) {
    if (r >= 0x30 && r <= 0x39) {
      b.writeCharCode(r); // 0-9
    } else if (r == 0x58 || r == 0x78) {
      b.writeCharCode(0x58); // X / x -> X
    }
  }
  return b.toString();
}

/// ISBN-10: sum(d_i * (10-i)) ≡ 0 (mod 11); the final digit may be 'X' (= 10).
bool _valid10(String s) {
  var sum = 0;
  for (var i = 0; i < 10; i++) {
    final c = s.codeUnitAt(i);
    int d;
    if (c >= 0x30 && c <= 0x39) {
      d = c - 0x30;
    } else if (c == 0x58 && i == 9) {
      d = 10; // X is only legal as the final check digit
    } else {
      return false;
    }
    sum += d * (10 - i);
  }
  return sum % 11 == 0;
}

/// ISBN-13 / EAN-13: sum(d_i * w_i) ≡ 0 (mod 10) with weights alternating 1,3.
bool _valid13(String s) {
  var sum = 0;
  for (var i = 0; i < 13; i++) {
    final c = s.codeUnitAt(i);
    if (c < 0x30 || c > 0x39) return false;
    var d = c - 0x30;
    if (i.isOdd) d *= 3;
    sum += d;
  }
  return sum % 10 == 0;
}

/// Converts a valid ISBN-10 to ISBN-13: prefix "978" to the first 9 digits and
/// recompute the EAN-13 check digit.
String _to13(String s10) {
  final body = '978${s10.substring(0, 9)}';
  var sum = 0;
  for (var i = 0; i < 12; i++) {
    var d = body.codeUnitAt(i) - 0x30;
    if (i.isOdd) d *= 3;
    sum += d;
  }
  final check = (10 - sum % 10) % 10;
  return '$body$check';
}
