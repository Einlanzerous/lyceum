// The series-number field's two directions (LYCM-129): what a stored position
// looks like as text, and what typed text means as a position.

/// A stored series position as field text: 4 → "4", 3.5 → "3.5", none → "".
String seriesIndexText(double? n) {
  if (n == null || !n.isFinite) return '';
  return n == n.truncateToDouble() ? n.toInt().toString() : n.toString();
}

/// The position a typed field means: "3" → 3, "3.5" → 3.5, and blank, junk or
/// negative → 0, which the server stores as "no position".
double parseSeriesIndex(String text) {
  final n = double.tryParse(text.trim());
  if (n == null || !n.isFinite || n < 0) return 0;
  return n;
}
