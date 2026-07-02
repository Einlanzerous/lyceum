import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'prefs.dart';

/// Local profile identity — a display name. Lyceum is a single-user, self-hosted
/// server with no accounts, so this is a persisted local label shown at the top
/// of Settings and used as the library avatar's initial. (The placeholder "R"
/// avatar stood for the default, "Reader".)
const _kNameKey = 'lyceum.profile_name';
const _kDefaultName = 'Reader';

class ProfileController extends Notifier<String> {
  @override
  String build() => ref.watch(prefsProvider).getString(_kNameKey)?.trim() ?? '';

  /// Persists the name as typed (empty is allowed — the avatar/label fall back
  /// to the default). Called live on each edit so nothing is lost if the user
  /// navigates away without an explicit blur.
  Future<void> set(String value) async {
    final name = value.trim();
    await ref.read(prefsProvider).setString(_kNameKey, name);
    state = name;
  }
}

final profileNameProvider =
    NotifierProvider<ProfileController, String>(ProfileController.new);

/// Uppercase first letter of the display name, for the avatar (falls back to
/// the default "Reader" initial when unset).
final profileInitialProvider = Provider<String>((ref) {
  final name = ref.watch(profileNameProvider).trim();
  return (name.isNotEmpty ? name[0] : _kDefaultName[0]).toUpperCase();
});

