import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'prefs.dart';

/// Reading typeface passed through to the epub.js reader. Mirrors
/// `web/src/reader/font.ts` (persisted under `lyceum.readingFont`,
/// default `publisher`).
enum ReadingFont {
  publisher,
  serif,
  sans;

  String get label => switch (this) {
        ReadingFont.publisher => 'Publisher',
        ReadingFont.serif => 'Serif',
        ReadingFont.sans => 'Sans',
      };
}

const _kFontKey = 'lyceum.readingFont';

class ReadingFontController extends Notifier<ReadingFont> {
  @override
  ReadingFont build() {
    final raw = ref.watch(prefsProvider).getString(_kFontKey);
    return ReadingFont.values.firstWhere(
      (f) => f.name == raw,
      orElse: () => ReadingFont.publisher,
    );
  }

  Future<void> set(ReadingFont font) async {
    await ref.read(prefsProvider).setString(_kFontKey, font.name);
    state = font;
  }
}

final readingFontProvider =
    NotifierProvider<ReadingFontController, ReadingFont>(
        ReadingFontController.new);
