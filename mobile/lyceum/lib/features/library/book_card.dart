import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../api/api_providers.dart';
import '../../api/models.dart';
import '../../theme/lyceum_colors.dart';
import '../../theme/lyceum_theme.dart';
import 'library_controller.dart';

String _pct(double v) => '${(v * 100).round()}%';

/// Long-press action sheet for a library tile: mark the book read/unread.
void _showReadMenu(BuildContext context, WidgetRef ref, Book book) {
  showModalBottomSheet<void>(
    context: context,
    builder: (ctx) => SafeArea(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          ListTile(
            leading: Icon(
              book.finished
                  ? Icons.remove_done_rounded
                  : Icons.done_all_rounded,
              color: context.lyc.brass,
            ),
            title: Text(book.finished ? 'Mark as unread' : 'Mark as read'),
            subtitle: Text(
              book.title,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
            ),
            onTap: () {
              Navigator.of(ctx).pop();
              ref
                  .read(libraryControllerProvider.notifier)
                  .setFinished(book.id, !book.finished);
            },
          ),
        ],
      ),
    ),
  );
}

/// Grid tile: a 2:3 cover (or generated fallback) with an optional progress
/// pill and a brass seam, plus title/author beneath. Mirrors `BookCard.vue`.
class BookCard extends ConsumerWidget {
  const BookCard({super.key, required this.book, this.pinned = false});
  final Book book;
  final bool pinned;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final lyc = context.lyc;
    final client = ref.watch(lyceumClientProvider);
    return GestureDetector(
      onTap: () => context.push('/reader/${book.id}'),
      onLongPress: () => _showReadMenu(context, ref, book),
      behavior: HitTestBehavior.opaque,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          AspectRatio(
            // Matches our cover source (Apple Books, ~366x600) so covers fill
            // edge-to-edge; see BoxFit.contain->cover note below.
            aspectRatio: 366 / 600,
            child: ClipRRect(
              borderRadius: BorderRadius.circular(LycRadii.cover),
              child: DecoratedBox(
                decoration: BoxDecoration(
                  // Backs the letterbox bars left by BoxFit.contain, matching
                  // the web card surface.
                  color: lyc.surfaceRaised,
                  boxShadow: [
                    BoxShadow(
                      color: Colors.black.withValues(alpha: 0.4),
                      blurRadius: 18,
                      offset: const Offset(0, 6),
                    ),
                  ],
                ),
                child: Stack(
                  fit: StackFit.expand,
                  children: [
                    if (book.hasCover)
                      // cover, with the card aspect matched to the source: covers
                      // fill edge-to-edge and any residual aspect difference crops
                      // the sides, never the top banner / bottom author bar.
                      Image.network(
                        client.coverUrl(book.id),
                        fit: BoxFit.cover,
                        errorBuilder: (_, _, _) => _FallbackCover(book: book),
                      )
                    else
                      _FallbackCover(book: book),
                    if (pinned && !book.finished)
                      const Positioned(top: 8, left: 8, child: ContinuePill()),
                    if (book.finished)
                      const Positioned(top: 8, right: 8, child: _ReadPill())
                    else if (book.progress != null)
                      Positioned(
                        top: 8,
                        right: 8,
                        child: _ProgressPill(value: book.progress!),
                      ),
                    if (!book.finished && book.progress != null)
                      Positioned(
                        left: 0,
                        right: 0,
                        bottom: 0,
                        child: _ProgressSeam(value: book.progress!),
                      ),
                  ],
                ),
              ),
            ),
          ),
          const SizedBox(height: 10),
          Text(
            book.title,
            maxLines: 2,
            overflow: TextOverflow.ellipsis,
            style: TextStyle(
              fontSize: 13,
              fontWeight: FontWeight.w700,
              color: lyc.text,
              height: 1.25,
            ),
          ),
          const SizedBox(height: 2),
          Text(
            book.author,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: TextStyle(fontSize: 11.5, color: lyc.dim),
          ),
        ],
      ),
    );
  }
}

/// List row: small thumb + title/author + progress %. Mirrors the web list view.
class BookListTile extends ConsumerWidget {
  const BookListTile({super.key, required this.book});
  final Book book;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final lyc = context.lyc;
    final client = ref.watch(lyceumClientProvider);
    return GestureDetector(
      onTap: () => context.push('/reader/${book.id}'),
      behavior: HitTestBehavior.opaque,
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 8),
        child: Row(
          children: [
            ClipRRect(
              borderRadius: BorderRadius.circular(4),
              child: SizedBox(
                width: 38,
                height: 56,
                child: book.hasCover
                    ? Image.network(
                        client.coverUrl(book.id),
                        fit: BoxFit.cover,
                        errorBuilder: (_, _, _) =>
                            _FallbackCover(book: book, compact: true),
                      )
                    : _FallbackCover(book: book, compact: true),
              ),
            ),
            const SizedBox(width: 14),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    book.title,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(
                      fontSize: 14.5,
                      fontWeight: FontWeight.w700,
                      color: lyc.text,
                    ),
                  ),
                  const SizedBox(height: 2),
                  Text(
                    book.author,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(fontSize: 12.5, color: lyc.dim),
                  ),
                ],
              ),
            ),
            if (book.progress != null)
              Text(
                _pct(book.progress!),
                style: TextStyle(
                  fontSize: 12.5,
                  fontWeight: FontWeight.w700,
                  color: lyc.brass,
                ),
              ),
          ],
        ),
      ),
    );
  }
}

/// Brass "Continue" chip marking the pinned current-read tile.
class ContinuePill extends StatelessWidget {
  const ContinuePill({super.key});
  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(
        color: lyc.brass,
        borderRadius: BorderRadius.circular(LycRadii.pill),
      ),
      child: Text(
        '▸ Continue',
        style: TextStyle(
          fontSize: 10.5,
          fontWeight: FontWeight.w700,
          color: lyc.onBrass,
        ),
      ),
    );
  }
}

/// Green "Read" chip shown on a book marked finished.
class _ReadPill extends StatelessWidget {
  const _ReadPill();
  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(
        color: context.lyc.success,
        borderRadius: BorderRadius.circular(LycRadii.pill),
      ),
      child: const Text(
        '✓ Read',
        style: TextStyle(
          fontSize: 10.5,
          fontWeight: FontWeight.w700,
          color: Colors.white,
        ),
      ),
    );
  }
}

class _ProgressPill extends StatelessWidget {
  const _ProgressPill({required this.value});
  final double value;

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(
        color: lyc.pillOnCover,
        borderRadius: BorderRadius.circular(LycRadii.pill),
      ),
      child: Text(
        _pct(value),
        style: TextStyle(
          fontSize: 11,
          fontWeight: FontWeight.w700,
          color: lyc.brassBright,
        ),
      ),
    );
  }
}

class _ProgressSeam extends StatelessWidget {
  const _ProgressSeam({required this.value});
  final double value;

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return SizedBox(
      height: 3,
      child: Row(
        children: [
          Expanded(
            flex: (value.clamp(0, 1) * 1000).round(),
            child: ColoredBox(color: lyc.brass),
          ),
          Expanded(
            flex: 1000 - (value.clamp(0, 1) * 1000).round(),
            child: ColoredBox(color: lyc.pillOnCover),
          ),
        ],
      ),
    );
  }
}

/// Generated cover tile when a book has no cover image (or it fails to load):
/// the title in Archivo 800 over a subtle diagonal hatch, with a brass tick.
class _FallbackCover extends StatelessWidget {
  const _FallbackCover({required this.book, this.compact = false});
  final Book book;
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return CustomPaint(
      painter: _HatchPainter(
        base: lyc.surfaceRaised,
        line: lyc.border,
        tick: book.progress != null ? lyc.brass : lyc.borderStrong,
      ),
      child: compact
          ? const SizedBox.expand()
          : Padding(
              padding: const EdgeInsets.all(14),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                mainAxisAlignment: MainAxisAlignment.end,
                children: [
                  Text(
                    book.title,
                    maxLines: 4,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(
                      fontFamily: kDisplayFont,
                      fontSize: 17,
                      height: 1.15,
                      fontWeight: FontWeight.w800,
                      color: lyc.text,
                    ),
                  ),
                  const SizedBox(height: 6),
                  Text(
                    book.author,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(fontSize: 11, color: lyc.muted),
                  ),
                ],
              ),
            ),
    );
  }
}

class _HatchPainter extends CustomPainter {
  _HatchPainter({required this.base, required this.line, required this.tick});
  final Color base;
  final Color line;
  final Color tick;

  @override
  void paint(Canvas canvas, Size size) {
    final rect = Offset.zero & size;
    canvas.drawRect(rect, Paint()..color = base);
    final linePaint = Paint()
      ..color = line
      ..strokeWidth = 1;
    const gap = 14.0;
    for (double x = -size.height; x < size.width; x += gap) {
      canvas.drawLine(
        Offset(x, size.height),
        Offset(x + size.height, 0),
        linePaint,
      );
    }
    // Top-left tick.
    canvas.drawRect(Rect.fromLTWH(0, 0, 18, 4), Paint()..color = tick);
  }

  @override
  bool shouldRepaint(_HatchPainter old) =>
      old.base != base || old.line != line || old.tick != tick;
}
