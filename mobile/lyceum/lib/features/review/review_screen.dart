import 'package:file_selector/file_selector.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../api/api_providers.dart';
import '../../api/client.dart';
import '../../api/models.dart';
import '../../theme/lyceum_colors.dart';
import '../../widgets/cover_image.dart';
import '../library/library_controller.dart';
import 'review_controller.dart';
import 'review_flags.dart';

/// The ingest-QC review queue (LYCM-72, the native half of LYCM-58).
///
/// Books that tripped an ingest detector are held off the shelf and land here.
/// For each, correct the title/author, replace the cover, then approve it onto
/// the shelf — or drop it.
class ReviewScreen extends ConsumerWidget {
  const ReviewScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final lyc = context.lyc;
    final queue = ref.watch(reviewControllerProvider);

    return Scaffold(
      body: SafeArea(
        // See LibraryScreen: the WebView reader can zero the top inset, and this
        // screen must never slide under the status bar.
        minimum: EdgeInsets.only(top: MediaQuery.viewPaddingOf(context).top),
        child: RefreshIndicator(
          onRefresh: () =>
              ref.read(reviewControllerProvider.notifier).refresh(),
          child: ListView(
            padding: const EdgeInsets.fromLTRB(20, 16, 20, 48),
            children: [
              _BackPill(onTap: () => context.go('/')),
              const SizedBox(height: 24),
              Text(
                'INGESTION',
                style: TextStyle(
                  fontSize: 11.5,
                  fontWeight: FontWeight.w700,
                  letterSpacing: 3,
                  color: lyc.brass,
                ),
              ),
              const SizedBox(height: 6),
              Text(
                'Review queue',
                style: Theme.of(context).textTheme.headlineLarge,
              ),
              const SizedBox(height: 10),
              Text(
                'New books that tripped a quality check are held here. Fix the '
                'details, then approve them onto the shelf.',
                style: TextStyle(color: lyc.muted, height: 1.4),
              ),
              const SizedBox(height: 24),
              ...switch (queue) {
                AsyncLoading() => [const _Note('Loading…')],
                AsyncError(:final error) => [
                  _Note(_message(error), isError: true),
                ],
                AsyncData(:final value) when value.isEmpty => [
                  const _Note(
                    'Nothing to review — every ingested book is on the shelf.',
                  ),
                ],
                AsyncData(:final value) => [
                  for (final b in value) ...[
                    // Keyed by id, not position. Without this Flutter reconciles
                    // the rows by index, so approving one book hands its State —
                    // including the edit fields, which initialize once — to the
                    // book that moves up into its slot. Saving that card would
                    // then write the approved book's title onto a different one.
                    _ReviewCard(key: ValueKey(b.id), book: b),
                    const SizedBox(height: 16),
                  ],
                ],
              },
            ],
          ),
        ),
      ),
    );
  }
}

/// The server sends plain-text bodies, so an [ApiException] already carries the
/// sentence worth showing. Anything else gets a generic line rather than a Dart
/// type name.
String _message(Object error) => switch (error) {
  ApiException(:final message) when message.isNotEmpty => message,
  _ => 'Could not load the review queue.',
};

class _ReviewCard extends ConsumerStatefulWidget {
  const _ReviewCard({super.key, required this.book});
  final Book book;

  @override
  ConsumerState<_ReviewCard> createState() => _ReviewCardState();
}

class _ReviewCardState extends ConsumerState<_ReviewCard> {
  late final TextEditingController _title = TextEditingController(
    text: widget.book.title,
  );
  late final TextEditingController _author = TextEditingController(
    text: widget.book.author,
  );

  /// The action in flight, or null when idle. One at a time per card: the
  /// buttons all mutate the same row, and letting two race would leave the card
  /// showing whichever reply landed last.
  String? _busy;
  String? _error;

  /// Bumped whenever this card changes the cover bytes.
  ///
  /// A replaced or re-fetched cover keeps its URL — the id is stable and the
  /// server writes new bytes underneath it — and Flutter's image cache is keyed
  /// on (url, scale), so without a changing query the screen keeps showing the
  /// old picture and the re-fetch looks like it did nothing. Which is the whole
  /// point of the button for a book held on low_quality_cover.
  int _coverBust = 0;

  @override
  void dispose() {
    _title.dispose();
    _author.dispose();
    super.dispose();
  }

  /// Runs one card action, holding the card busy and surfacing any failure
  /// inline rather than as a toast that outlives the row it was about.
  /// [progress] and [verb] are separate because one string cannot be both:
  /// "Working… (cover)" and "Could not cover." are what a single label buys.
  Future<void> _run(
    String progress,
    String verb,
    Future<void> Function() action,
  ) async {
    setState(() {
      _busy = progress;
      _error = null;
    });
    try {
      await action();
    } catch (e) {
      if (mounted) setState(() => _error = _actionMessage(e, verb));
    } finally {
      if (mounted) setState(() => _busy = null);
    }
  }

  String _actionMessage(Object e, String verb) => switch (e) {
    // 503 is a server built without an art source, not a transient fault, so
    // "try again" would be the wrong advice.
    ApiException(isUnavailable: true) =>
      'This server has no cover art source configured.',
    ApiException(isNotFound: true) when verb.contains('cover') =>
      'No cover art found for this book.',
    ApiException(:final message) when message.isNotEmpty => message,
    // Timeouts and socket errors land here — the likeliest failure of the lot.
    _ => 'Could not $verb.',
  };

  /// Approve and delete both take the book out of the queue, and approve puts it
  /// on the shelf — so the library is re-read either way rather than left
  /// showing a stale grid.
  Future<void> _leaveQueue(
    String progress,
    String verb,
    Future<void> Function() action,
  ) => _run(progress, verb, () async {
    await action();
    ref.invalidate(libraryControllerProvider);
  });

  Future<void> _pickCover() async {
    const images = XTypeGroup(
      label: 'images',
      extensions: ['jpg', 'jpeg', 'png', 'webp'],
      mimeTypes: ['image/jpeg', 'image/png', 'image/webp'],
    );
    final file = await openFile(acceptedTypeGroups: const [images]);
    if (file == null) return; // cancelled
    // Bytes rather than a path: Android hands back a content:// URI that is not
    // a filesystem path, and reading it here keeps the client's file handling to
    // one shape.
    final bytes = await file.readAsBytes();
    if (!mounted) return;
    await _run('Uploading the cover', 'upload the cover', () async {
      await ref
          .read(reviewControllerProvider.notifier)
          .replaceCover(widget.book.id, filename: file.name, bytes: bytes);
      if (mounted) setState(() => _coverBust++);
    });
  }

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    final b = widget.book;
    final busy = _busy != null;

    return Container(
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: lyc.surface,
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: lyc.border),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              _Cover(book: b, width: 72, bust: _coverBust),
              const SizedBox(width: 14),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Wrap(
                      spacing: 6,
                      runSpacing: 6,
                      children: [
                        for (final f in b.reviewFlags)
                          _Chip(reviewFlagLabel(f)),
                      ],
                    ),
                    const SizedBox(height: 10),
                    _Field(label: 'Title', controller: _title, enabled: !busy),
                    const SizedBox(height: 8),
                    _Field(
                      label: 'Author',
                      controller: _author,
                      enabled: !busy,
                    ),
                  ],
                ),
              ),
            ],
          ),
          if (holdsPossibleDuplicate(b.reviewFlags)) ...[
            const SizedBox(height: 14),
            _DuplicatePanel(book: b),
          ],
          const SizedBox(height: 14),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              _Action(
                'Save details',
                enabled: !busy,
                onTap: () => _run(
                  'Saving the details',
                  'save the details',
                  () => ref
                      .read(reviewControllerProvider.notifier)
                      .saveMeta(b.id, _title.text.trim(), _author.text.trim()),
                ),
              ),
              _Action(
                'Re-fetch cover',
                enabled: !busy,
                onTap: () => _run(
                  'Re-fetching the cover',
                  're-fetch the cover',
                  () async {
                    await ref
                        .read(reviewControllerProvider.notifier)
                        .refetchCover(b.id);
                    if (mounted) setState(() => _coverBust++);
                  },
                ),
              ),
              _Action('Upload cover', enabled: !busy, onTap: _pickCover),
              _Action(
                // "Keep both" when the hold is about a duplicate: the decision
                // is between two files, not about whether this one is junk.
                holdsPossibleDuplicate(b.reviewFlags) ? 'Keep both' : 'Approve',
                enabled: !busy,
                primary: true,
                onTap: () => _leaveQueue(
                  'Approving',
                  'approve this book',
                  () =>
                      ref.read(reviewControllerProvider.notifier).approve(b.id),
                ),
              ),
              _Action(
                holdsPossibleDuplicate(b.reviewFlags)
                    ? 'Delete this copy'
                    : 'Delete',
                enabled: !busy,
                danger: true,
                onTap: () async {
                  final ok = await _confirmDelete(context, b.title);
                  if (!ok) return;
                  await _leaveQueue(
                    'Deleting',
                    'delete this book',
                    () => ref
                        .read(reviewControllerProvider.notifier)
                        .discard(b.id),
                  );
                },
              ),
            ],
          ),
          if (_busy != null) ...[
            const SizedBox(height: 10),
            Text('$_busy…', style: TextStyle(color: lyc.muted)),
          ] else if (_error != null) ...[
            const SizedBox(height: 10),
            Text(_error!, style: TextStyle(color: lyc.error)),
          ],
        ],
      ),
    );
  }
}

Future<bool> _confirmDelete(BuildContext context, String title) async {
  final ok = await showDialog<bool>(
    context: context,
    builder: (ctx) => AlertDialog(
      title: const Text('Delete this book?'),
      content: Text(
        '“$title” and its stored file are removed for good. This cannot be '
        'undone.',
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(ctx).pop(false),
          child: const Text('Cancel'),
        ),
        TextButton(
          onPressed: () => Navigator.of(ctx).pop(true),
          child: const Text('Delete'),
        ),
      ],
    ),
  );
  return ok ?? false;
}

/// The side-by-side for a suspected duplicate (LYCM-113): two files of one work
/// are often deliberate — a better scan, another translation — so the question
/// is "keep both?" and it cannot be answered from one cover alone.
class _DuplicatePanel extends ConsumerWidget {
  const _DuplicatePanel({required this.book});
  final Book book;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final lyc = context.lyc;
    final id = book.duplicateOf;
    // No pointer means the book this matched is already gone: the server nulls
    // the column on delete while the flag stays, which is the common end state
    // once someone resolves the pair from another device.
    final match = id == null
        ? const AsyncData<Book?>(null)
        : ref.watch(duplicateOfProvider(id));

    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        borderRadius: BorderRadius.circular(10),
        border: Border.all(color: lyc.border),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            'This looks like another copy of a book you already have.',
            style: TextStyle(color: lyc.muted, height: 1.4),
          ),
          const SizedBox(height: 12),
          _body(match, lyc),
        ],
      ),
    );
  }

  /// The panel's body, in one of four states.
  ///
  /// Written against hasError/hasValue rather than the AsyncValue subtype:
  /// riverpod 3 retries a failed provider by default, so a failed lookup spends
  /// its time in loading-or-retrying states that carry the error alongside.
  /// Matching on `AsyncError()` alone left a 500 reading "Loading the other
  /// copy…" indefinitely.
  Widget _body(AsyncValue<Book?> match, LyceumPalette lyc) {
    // Checked before the value: "it was deleted, go ahead and approve" is
    // confident advice about a book the server is still holding as a duplicate,
    // and giving it because a request failed would be advice from ignorance.
    if (match.hasError) {
      return Text(
        "Couldn't load the other copy just now, so this pair can't be "
        'compared. Pull to refresh.',
        style: TextStyle(color: lyc.error, height: 1.4),
      );
    }
    if (!match.hasValue) {
      return Text(
        'Loading the other copy…',
        style: TextStyle(color: lyc.muted),
      );
    }
    final other = match.value;
    if (other == null) {
      return Text(
        'The book this matched has since been deleted, so there is probably '
        'nothing left to decide — approve it onto the shelf.',
        style: TextStyle(color: lyc.muted, height: 1.4),
      );
    }
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Expanded(
          child: _DuplicateSide(tag: 'Already on the shelf', book: other),
        ),
        const SizedBox(width: 12),
        Expanded(
          child: _DuplicateSide(
            tag: 'This one, held',
            book: book,
            highlight: true,
          ),
        ),
      ],
    );
  }
}

class _DuplicateSide extends StatelessWidget {
  const _DuplicateSide({
    required this.tag,
    required this.book,
    this.highlight = false,
  });
  final String tag;
  final Book book;
  final bool highlight;

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          tag.toUpperCase(),
          style: TextStyle(
            fontSize: 10,
            letterSpacing: 1.2,
            fontWeight: FontWeight.w700,
            color: highlight ? lyc.brass : lyc.muted,
          ),
        ),
        const SizedBox(height: 6),
        _Cover(book: book, width: 76),
        const SizedBox(height: 8),
        Text(book.title, style: const TextStyle(fontWeight: FontWeight.w600)),
        Text(book.author, style: TextStyle(color: lyc.muted, fontSize: 13)),
      ],
    );
  }
}

class _Cover extends ConsumerWidget {
  const _Cover({required this.book, required this.width, this.bust = 0});
  final Book book;
  final double width;

  /// Cache-buster for a cover this session has just rewritten. Left at 0 — and
  /// so off the URL entirely — for a book nothing has touched, which keeps the
  /// cache key identical to the one the library grid already fetched under.
  final int bust;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final lyc = context.lyc;
    final placeholder = Container(
      alignment: Alignment.center,
      color: lyc.surfaceRaised,
      child: Text(
        'No cover',
        textAlign: TextAlign.center,
        style: TextStyle(color: lyc.muted, fontSize: 11),
      ),
    );
    return ClipRRect(
      borderRadius: BorderRadius.circular(6),
      child: SizedBox(
        width: width,
        height: width * 600 / 366, // the shelf's cover aspect
        child: book.hasCover
            ? CoverImage(
                url: [
                  ref.watch(lyceumClientProvider).coverUrl(book.id),
                  if (bust > 0) '?v=$bust',
                ].join(),
                fallback: placeholder,
              )
            : placeholder,
      ),
    );
  }
}

class _Chip extends StatelessWidget {
  const _Chip(this.label);
  final String label;

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
      decoration: BoxDecoration(
        color: lyc.brassWash,
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text(
        label,
        style: TextStyle(
          color: lyc.brassBright,
          fontSize: 11.5,
          fontWeight: FontWeight.w600,
        ),
      ),
    );
  }
}

class _Field extends StatelessWidget {
  const _Field({
    required this.label,
    required this.controller,
    required this.enabled,
  });
  final String label;
  final TextEditingController controller;
  final bool enabled;

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return TextField(
      controller: controller,
      enabled: enabled,
      style: const TextStyle(fontSize: 14),
      decoration: InputDecoration(
        labelText: label,
        isDense: true,
        border: OutlineInputBorder(
          borderRadius: BorderRadius.circular(8),
          borderSide: BorderSide(color: lyc.border),
        ),
      ),
    );
  }
}

class _Action extends StatelessWidget {
  const _Action(
    this.label, {
    required this.enabled,
    required this.onTap,
    this.primary = false,
    this.danger = false,
  });
  final String label;
  final bool enabled;
  final VoidCallback onTap;
  final bool primary;
  final bool danger;

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    final fg = danger
        ? lyc.error
        : primary
        ? lyc.onBrass
        : lyc.text;
    return TextButton(
      onPressed: enabled ? onTap : null,
      style: TextButton.styleFrom(
        foregroundColor: fg,
        backgroundColor: primary ? lyc.brass : Colors.transparent,
        side: primary ? null : BorderSide(color: lyc.borderStrong),
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
      ),
      child: Text(label, style: const TextStyle(fontSize: 13.5)),
    );
  }
}

class _Note extends StatelessWidget {
  const _Note(this.text, {this.isError = false});
  final String text;
  final bool isError;

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 8),
      child: Text(
        text,
        style: TextStyle(color: isError ? lyc.error : lyc.muted, height: 1.4),
      ),
    );
  }
}

class _BackPill extends StatelessWidget {
  const _BackPill({required this.onTap});
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return Align(
      alignment: Alignment.centerLeft,
      child: TextButton.icon(
        onPressed: onTap,
        icon: const Icon(Icons.arrow_back, size: 18),
        label: const Text('Library'),
        style: TextButton.styleFrom(foregroundColor: lyc.muted),
      ),
    );
  }
}
