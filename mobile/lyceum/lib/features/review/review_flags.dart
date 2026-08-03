/// Human labels for the backend's stable ingest-QC issue codes (LYCM-58), kept
/// in step with `FLAG_LABELS` in `web/src/views/ReviewView.vue`.
const Map<String, String> reviewFlagLabels = {
  'no_isbn': 'No ISBN',
  'no_cover': 'No cover',
  'low_quality_cover': 'Poor cover',
  'suspicious_title': 'Odd title',
  'possible_duplicate': 'Possible duplicate',
};

/// The label for one flag code.
///
/// An unrecognised code falls through to itself rather than being dropped or
/// replaced with "Unknown". A self-hosted server can easily be newer than the
/// app on someone's phone, and `low_quality_cover` reads perfectly well as a
/// reason a book was held — where a silently missing chip would leave the
/// screen claiming nothing was wrong with a book it is refusing to publish.
String reviewFlagLabel(String code) => reviewFlagLabels[code] ?? code;

/// Whether a queued book is held as a suspected copy of another (LYCM-113).
///
/// Keyed on the flag rather than on `duplicate_of`, which the server nulls once
/// the matched book is deleted. Gating the comparison panel on the pointer hid
/// it in exactly the case it exists to explain — the same bug the web surface
/// shipped and fixed.
bool holdsPossibleDuplicate(List<String> flags) =>
    flags.contains('possible_duplicate');
