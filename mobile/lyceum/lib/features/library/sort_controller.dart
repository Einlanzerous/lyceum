import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../prefs/prefs.dart';
import 'shelf.dart';

/// The library sort order (LYCM-62), persisted like the web app's localStorage
/// preference so it survives restarts. Stored as two keys under `lyceum.*`.
const _kSortKey = 'lyceum.library.sortKey';
const _kSortAsc = 'lyceum.library.sortAsc';

class SortController extends Notifier<SortState> {
  @override
  SortState build() {
    final prefs = ref.watch(prefsProvider);
    final key = sortKeyFromStorage(prefs.getString(_kSortKey));
    final asc = prefs.getBool(_kSortAsc) ?? key.defaultAscending;
    return SortState(key: key, ascending: asc);
  }

  Future<void> setKey(SortKey key) async {
    // Adopt the key's natural direction on switch, matching the web default.
    final next = SortState(key: key, ascending: key.defaultAscending);
    await _persist(next);
    state = next;
  }

  Future<void> toggleDirection() async {
    final next = state.copyWith(ascending: !state.ascending);
    await _persist(next);
    state = next;
  }

  Future<void> _persist(SortState s) async {
    final prefs = ref.read(prefsProvider);
    await prefs.setString(_kSortKey, s.key.storageValue);
    await prefs.setBool(_kSortAsc, s.ascending);
  }
}

final sortControllerProvider =
    NotifierProvider<SortController, SortState>(SortController.new);
