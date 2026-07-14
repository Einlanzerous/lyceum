import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_providers.dart';

/// A book cover, fetched with this device's session attached.
///
/// `/books/{id}/cover` is a gated route now (LYCM-801), and every cover on the
/// shelf would 401 without a credential. The browser has a genuinely hard time
/// with this — an `<img>` tag cannot send an `Authorization` header, which is why
/// the web build needs a session cookie and the Wails shell needs the whole
/// object-URL apparatus in `web/src/api/coverSrc.ts`. Flutter's `Image.network`
/// simply takes headers, so the entire problem is this widget.
class CoverImage extends ConsumerWidget {
  const CoverImage({
    super.key,
    required this.url,
    required this.fallback,
    this.fit = BoxFit.cover,
  });

  final String url;

  /// What to show when the cover won't load. A missing cover is a placeholder,
  /// never an error worth surfacing.
  final Widget fallback;
  final BoxFit fit;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Image.network(
      url,
      fit: fit,
      headers: ref.watch(coverHeadersProvider),
      errorBuilder: (_, _, _) => fallback,
    );
  }
}
