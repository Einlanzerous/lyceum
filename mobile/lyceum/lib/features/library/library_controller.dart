import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../api/api_providers.dart';
import '../../api/models.dart';
import '../../api/server_store.dart';

/// Loads and refreshes the digital shelf. Rebuilds automatically when the
/// server URL changes (via [lyceumClientProvider]). Books are added on the
/// server via its ingestion pipeline, so there is no in-app upload.
class LibraryController extends AsyncNotifier<List<Book>> {
  @override
  Future<List<Book>> build() async {
    if (!ref.watch(hasBackendProvider)) return const [];
    return ref.watch(lyceumClientProvider).listLibrary();
  }

  Future<void> refresh() async {
    state = const AsyncLoading();
    state = await AsyncValue.guard(
      () => ref.read(lyceumClientProvider).listLibrary(),
    );
  }
}

final libraryControllerProvider =
    AsyncNotifierProvider<LibraryController, List<Book>>(LibraryController.new);

/// Grid vs list shelf layout (session-scoped, defaults to grid like the web).
class ViewModeController extends Notifier<bool> {
  @override
  bool build() => true; // true = grid

  void toggle() => state = !state;
}

final gridViewProvider =
    NotifierProvider<ViewModeController, bool>(ViewModeController.new);
