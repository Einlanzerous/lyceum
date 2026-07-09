import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../api/api_providers.dart';
import '../../api/models.dart';
import '../../theme/lyceum_colors.dart';
import '../../theme/lyceum_theme.dart';
import 'book_card.dart';
import 'shelf.dart';

String _pct(double v) => '${(v * 100).round()}%';

/// Grid tile for a rolled-up series (LYCM-36): a fanned stack of covers with a
/// count pill and aggregate progress. Tapping opens the members sheet — the
/// mobile take on the web's inline drawer.
class SeriesTile extends ConsumerWidget {
  const SeriesTile({super.key, required this.series, this.continueBookId});
  final SeriesGroup series;

  /// When set, this series is the pinned "current read": the Continue chip jumps
  /// straight into that volume instead of opening the members sheet.
  final int? continueBookId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final lyc = context.lyc;
    final client = ref.watch(lyceumClientProvider);
    return GestureDetector(
      onTap: () => showSeriesSheet(context, series),
      behavior: HitTestBehavior.opaque,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          AspectRatio(
            aspectRatio: 366 / 600,
            child: Stack(
              clipBehavior: Clip.none,
              children: [
                // Two offset layers fan out behind the top cover.
                Positioned(
                  left: 10,
                  top: 7,
                  right: -6,
                  bottom: -4,
                  child: Transform.rotate(
                    angle: 0.05,
                    child: _stackLayer(lyc.panel, lyc.border),
                  ),
                ),
                Positioned(
                  left: 5,
                  top: 3.5,
                  right: -2,
                  bottom: -1,
                  child: Transform.rotate(
                    angle: 0.026,
                    child: _stackLayer(lyc.surfaceRaised, lyc.border),
                  ),
                ),
                ClipRRect(
                  borderRadius: BorderRadius.circular(LycRadii.cover),
                  child: DecoratedBox(
                    decoration: BoxDecoration(color: lyc.surfaceRaised),
                    child: Stack(
                      fit: StackFit.expand,
                      children: [
                        if (series.coverBook.hasCover)
                          Image.network(
                            client.coverUrl(series.coverBook.id),
                            fit: BoxFit.cover,
                            errorBuilder: (_, _, _) =>
                                _SeriesFallback(name: series.name),
                          )
                        else
                          _SeriesFallback(name: series.name),
                        Positioned(
                          top: 8,
                          left: 8,
                          child: _CountPill(count: series.members.length),
                        ),
                        if (continueBookId != null)
                          Positioned(
                            top: 8,
                            right: 8,
                            child: GestureDetector(
                              onTap: () =>
                                  context.push('/reader/$continueBookId'),
                              child: const ContinuePill(),
                            ),
                          ),
                        Positioned(
                          left: 0,
                          right: 0,
                          bottom: 0,
                          child: _Seam(value: series.progress),
                        ),
                      ],
                    ),
                  ),
                ),
              ],
            ),
          ),
          const SizedBox(height: 10),
          Text(
            series.name,
            maxLines: 2,
            overflow: TextOverflow.ellipsis,
            style: TextStyle(
              fontSize: 13,
              fontWeight: FontWeight.w700,
              color: lyc.brass,
              height: 1.25,
            ),
          ),
          const SizedBox(height: 2),
          Text(
            'Series · ${_pct(series.progress)}',
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: TextStyle(fontSize: 11.5, color: lyc.dim),
          ),
        ],
      ),
    );
  }

  Widget _stackLayer(Color color, Color border) => DecoratedBox(
    decoration: BoxDecoration(
      color: color,
      borderRadius: BorderRadius.circular(LycRadii.cover),
      border: Border.all(color: border),
    ),
  );
}

class _CountPill extends StatelessWidget {
  const _CountPill({required this.count});
  final int count;
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
        '◲ $count',
        style: TextStyle(
          fontSize: 11,
          fontWeight: FontWeight.w700,
          color: lyc.brassBright,
        ),
      ),
    );
  }
}

class _Seam extends StatelessWidget {
  const _Seam({required this.value});
  final double value;
  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    final filled = (value.clamp(0, 1) * 1000).round();
    return SizedBox(
      height: 3,
      child: Row(
        children: [
          Expanded(
            flex: filled,
            child: ColoredBox(color: lyc.brass),
          ),
          Expanded(
            flex: 1000 - filled,
            child: ColoredBox(color: lyc.pillOnCover),
          ),
        ],
      ),
    );
  }
}

class _SeriesFallback extends StatelessWidget {
  const _SeriesFallback({required this.name});
  final String name;
  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return Container(
      color: lyc.surfaceRaised,
      padding: const EdgeInsets.all(14),
      alignment: Alignment.center,
      child: Text(
        name,
        maxLines: 4,
        textAlign: TextAlign.center,
        overflow: TextOverflow.ellipsis,
        style: TextStyle(
          fontFamily: kDisplayFont,
          fontSize: 16,
          height: 1.1,
          fontWeight: FontWeight.w800,
          color: lyc.text,
        ),
      ),
    );
  }
}

/// Modal sheet listing a series' volumes in reading order, with a resume
/// shortcut and per-volume status. Tapping a volume opens the reader.
Future<void> showSeriesSheet(BuildContext context, SeriesGroup series) {
  return showModalBottomSheet<void>(
    context: context,
    isScrollControlled: true,
    backgroundColor: Colors.transparent,
    builder: (_) => _SeriesSheet(series: series),
  );
}

class _SeriesSheet extends ConsumerWidget {
  const _SeriesSheet({required this.series});
  final SeriesGroup series;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final lyc = context.lyc;
    final client = ref.watch(lyceumClientProvider);
    final resumeAt = resumeIndex(series.members);
    final resumeBook = series.members[resumeAt];

    void open(int bookId) {
      Navigator.of(context).pop();
      context.push('/reader/$bookId');
    }

    return DraggableScrollableSheet(
      expand: false,
      initialChildSize: 0.6,
      minChildSize: 0.4,
      maxChildSize: 0.92,
      builder: (context, scrollController) => Container(
        decoration: BoxDecoration(
          color: lyc.panel,
          borderRadius: const BorderRadius.vertical(top: Radius.circular(18)),
          border: Border.all(color: lyc.brass.withValues(alpha: 0.35)),
        ),
        child: Column(
          children: [
            const SizedBox(height: 10),
            Container(
              width: 40,
              height: 4,
              decoration: BoxDecoration(
                color: lyc.borderStrong,
                borderRadius: BorderRadius.circular(2),
              ),
            ),
            Padding(
              padding: const EdgeInsets.fromLTRB(20, 14, 20, 10),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          series.name,
                          style: Theme.of(context).textTheme.titleLarge,
                        ),
                        const SizedBox(height: 2),
                        Text(
                          '${series.members.length} books · ${series.author}',
                          style: TextStyle(
                            fontSize: 11.5,
                            letterSpacing: 1,
                            color: lyc.brassBright,
                          ),
                        ),
                      ],
                    ),
                  ),
                  FilledButton(
                    onPressed: () => open(resumeBook.id),
                    child: Text('Resume book ${resumeAt + 1}'),
                  ),
                ],
              ),
            ),
            Divider(height: 1, color: lyc.border),
            Expanded(
              child: ListView.separated(
                controller: scrollController,
                padding: const EdgeInsets.fromLTRB(20, 8, 20, 28),
                itemCount: series.members.length,
                separatorBuilder: (_, _) =>
                    Divider(height: 1, color: lyc.border),
                itemBuilder: (context, i) {
                  final b = series.members[i];
                  return _MemberRow(
                    index: i,
                    book: b,
                    coverUrl: b.hasCover ? client.coverUrl(b.id) : null,
                    onTap: () => open(b.id),
                  );
                },
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _MemberRow extends StatelessWidget {
  const _MemberRow({
    required this.index,
    required this.book,
    required this.coverUrl,
    required this.onTap,
  });
  final int index;
  final Book book;
  final String? coverUrl;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    final status = memberStatus(book);
    final statusColor = status == MemberStatus.finished
        ? lyc.brassBright
        : lyc.dim;
    return GestureDetector(
      onTap: onTap,
      behavior: HitTestBehavior.opaque,
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 10),
        child: Row(
          children: [
            ClipRRect(
              borderRadius: BorderRadius.circular(4),
              child: SizedBox(
                width: 34,
                height: 50,
                child: coverUrl != null
                    ? Image.network(
                        coverUrl!,
                        fit: BoxFit.cover,
                        errorBuilder: (_, _, _) =>
                            ColoredBox(color: lyc.surfaceRaised),
                      )
                    : ColoredBox(color: lyc.surfaceRaised),
              ),
            ),
            const SizedBox(width: 12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    book.title,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(
                      fontSize: 14,
                      fontWeight: FontWeight.w700,
                      color: lyc.text,
                    ),
                  ),
                  const SizedBox(height: 2),
                  Text(
                    '${status.label} · Book ${index + 1}',
                    style: TextStyle(fontSize: 11.5, color: statusColor),
                  ),
                ],
              ),
            ),
            if (status == MemberStatus.inProgress && book.progress != null)
              Text(
                _pct(book.progress!),
                style: TextStyle(
                  fontSize: 12,
                  fontWeight: FontWeight.w700,
                  color: lyc.brassBright,
                ),
              ),
          ],
        ),
      ),
    );
  }
}
