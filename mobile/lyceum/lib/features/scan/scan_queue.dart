import 'dart:convert';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../../api/models.dart';
import '../../prefs/prefs.dart';

/// Persists the pending scan queue (LYCM-602) in [SharedPreferences] so an
/// un-sent batch survives an app restart or an offline session and can be
/// flushed later. ISBNs are kept unique and in scan order.
class ScanQueue {
  const ScanQueue(this._prefs);

  final SharedPreferences _prefs;
  static const _key = 'scan.pending.v1';

  /// The queued scans, oldest first. Empty when nothing is pending.
  List<ScannedIsbn> load() {
    final raw = _prefs.getString(_key);
    if (raw == null || raw.isEmpty) return const [];
    final decoded = jsonDecode(raw);
    if (decoded is! List) return const [];
    return decoded
        .whereType<Map<String, dynamic>>()
        .map(ScannedIsbn.fromJson)
        .toList(growable: false);
  }

  Future<void> _save(List<ScannedIsbn> scans) =>
      _prefs.setString(_key, jsonEncode(scans.map((s) => s.toJson()).toList()));

  /// Appends [scan] unless its ISBN is already queued. Returns the resulting
  /// list and whether it was newly added (false = duplicate, list unchanged).
  Future<({List<ScannedIsbn> scans, bool added})> add(ScannedIsbn scan) async {
    final scans = load();
    if (scans.any((s) => s.isbn == scan.isbn)) {
      return (scans: scans, added: false);
    }
    final next = [...scans, scan];
    await _save(next);
    return (scans: next, added: true);
  }

  /// Drops the scan with [isbn], returning the remaining list.
  Future<List<ScannedIsbn>> remove(String isbn) async {
    final next = load().where((s) => s.isbn != isbn).toList(growable: false);
    await _save(next);
    return next;
  }

  /// Clears the whole queue (after a successful flush).
  Future<void> clear() => _prefs.remove(_key);
}

/// The [ScanQueue] over the app's shared [SharedPreferences] handle.
final scanQueueProvider = Provider<ScanQueue>(
  (ref) => ScanQueue(ref.watch(prefsProvider)),
);
