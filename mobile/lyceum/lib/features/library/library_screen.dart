import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../api/api_providers.dart';
import '../../api/models.dart';
import '../../api/server_store.dart';
import '../../auth/auth_controller.dart';
import '../../auth/onboarding.dart';
import '../../theme/lyceum_colors.dart';
import '../../widgets/brand_mark.dart';
import '../auth/scan_onboarding.dart';
import '../review/review_controller.dart';
import '../settings/server_settings.dart';
import 'book_card.dart';
import 'library_controller.dart';
import 'library_search.dart';
import 'series_tile.dart';
import 'shelf.dart';
import 'sort_controller.dart';

class LibraryScreen extends ConsumerWidget {
  const LibraryScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final lyc = context.lyc;
    final hasBackend = ref.watch(hasBackendProvider);
    final grid = ref.watch(gridViewProvider);
    final library = ref.watch(libraryControllerProvider);

    return Scaffold(
      body: SafeArea(
        // Cheap insurance: never let the top bar sit under the status bar even
        // if an upstream inset is under-reported. (Normally viewPadding.top ==
        // padding.top, so this is a no-op.)
        minimum: EdgeInsets.only(top: MediaQuery.viewPaddingOf(context).top),
        child: Column(
          children: [
            const _TopBar(),
            Expanded(
              child: !hasBackend
                  ? _ConnectPrompt()
                  : RefreshIndicator(
                      color: lyc.brass,
                      onRefresh: () => ref
                          .read(libraryControllerProvider.notifier)
                          .refresh(),
                      child: library.when(
                        loading: () => const _LoadingShelf(),
                        error: (e, _) => _ErrorShelf(
                          message: '$e',
                          onRetry: () => ref
                              .read(libraryControllerProvider.notifier)
                              .refresh(),
                        ),
                        data: (books) => books.isEmpty
                            ? const _EmptyShelf()
                            : _Shelf(books: books, grid: grid),
                      ),
                    ),
            ),
          ],
        ),
      ),
    );
  }
}

class _TopBar extends ConsumerWidget {
  const _TopBar();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final lyc = context.lyc;
    // The avatar letter now comes from the account, not a local label — so it is
    // the same letter on every device this person signs in on.
    final initial = ref.watch(authControllerProvider).initial;
    return Container(
      padding: const EdgeInsets.fromLTRB(18, 12, 14, 12),
      decoration: BoxDecoration(
        border: Border(bottom: BorderSide(color: lyc.border)),
      ),
      child: Row(
        children: [
          const BrandMark(),
          const Spacer(),
          if (ref.watch(hasBackendProvider)) ...[
            // Only offered when something is actually waiting. An always-present
            // entry to a screen that reads "nothing to review" most of the time
            // is a permanent nudge about a job that is already done.
            if (ref.watch(pendingReviewCountProvider) > 0) ...[
              _IconPill(
                icon: Icons.rate_review_outlined,
                badge: ref.watch(pendingReviewCountProvider),
                onTap: () => context.push('/review'),
              ),
              const SizedBox(width: 10),
            ],
            _IconPill(
              icon: Icons.qr_code_scanner,
              onTap: () => context.push('/scan'),
            ),
            const SizedBox(width: 10),
          ],
          GestureDetector(
            onTap: () => context.push('/settings'),
            child: CircleAvatar(
              radius: 18,
              backgroundColor: lyc.brassWash,
              child: Text(
                initial,
                style: TextStyle(
                  color: lyc.brassBright,
                  fontWeight: FontWeight.w700,
                  fontSize: 14,
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

/// Small circular icon button (used for the grid/list toggle in the header).
///
/// [badge] draws a count over the corner when non-null and positive — the review
/// queue's "how many are waiting".
class _IconPill extends StatelessWidget {
  const _IconPill({required this.icon, required this.onTap, this.badge});
  final IconData icon;
  final VoidCallback onTap;
  final int? badge;
  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    final count = badge ?? 0;
    return GestureDetector(
      onTap: onTap,
      child: Stack(
        clipBehavior: Clip.none,
        children: [
          _pill(lyc),
          if (count > 0)
            Positioned(
              top: -4,
              right: -4,
              child: Container(
                padding: const EdgeInsets.symmetric(horizontal: 5, vertical: 2),
                constraints: const BoxConstraints(minWidth: 18),
                decoration: BoxDecoration(
                  color: lyc.brass,
                  borderRadius: BorderRadius.circular(999),
                ),
                child: Text(
                  '$count',
                  textAlign: TextAlign.center,
                  style: TextStyle(
                    color: lyc.onBrass,
                    fontSize: 11,
                    fontWeight: FontWeight.w700,
                  ),
                ),
              ),
            ),
        ],
      ),
    );
  }

  Widget _pill(LyceumPalette lyc) {
    return Container(
      width: 40,
      height: 40,
      decoration: BoxDecoration(
        shape: BoxShape.circle,
        border: Border.all(color: lyc.borderStrong),
      ),
      child: Icon(icon, size: 18, color: lyc.muted),
    );
  }
}

class _Header extends ConsumerWidget {
  const _Header({required this.books});
  final List<Book> books;
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final lyc = context.lyc;
    final grid = ref.watch(gridViewProvider);
    final sort = ref.watch(sortControllerProvider);
    return Row(
      crossAxisAlignment: CrossAxisAlignment.center,
      children: [
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                'YOUR LIBRARY',
                style: TextStyle(
                  fontSize: 11.5,
                  fontWeight: FontWeight.w700,
                  letterSpacing: 3,
                  color: lyc.brass,
                ),
              ),
              const SizedBox(height: 6),
              Text(
                'All Books',
                style: Theme.of(context).textTheme.headlineLarge,
              ),
              const SizedBox(height: 4),
              Text(
                '${books.length} on the shelf',
                style: TextStyle(fontSize: 13, color: lyc.dim),
              ),
            ],
          ),
        ),
        // Controls: sort key + direction, search, and the grid/list toggle.
        _SortMenu(sort: sort),
        const SizedBox(width: 8),
        _IconPill(
          icon: sort.ascending
              ? Icons.arrow_upward_rounded
              : Icons.arrow_downward_rounded,
          onTap: () =>
              ref.read(sortControllerProvider.notifier).toggleDirection(),
        ),
        const SizedBox(width: 8),
        _IconPill(
          icon: Icons.search_rounded,
          onTap: () => _openSearch(context, ref, books, sort),
        ),
        const SizedBox(width: 8),
        _IconPill(
          icon: grid ? Icons.view_list_rounded : Icons.grid_view_rounded,
          onTap: () => ref.read(gridViewProvider.notifier).toggle(),
        ),
      ],
    );
  }

  void _openSearch(
    BuildContext context,
    WidgetRef ref,
    List<Book> books,
    SortState sort,
  ) {
    final client = ref.read(lyceumClientProvider);
    showSearch<void>(
      context: context,
      delegate: LibrarySearchDelegate(
        books: books,
        sort: sort,
        coverUrlOf: client.coverUrl,
        onOpen: (id) => context.push('/reader/$id'),
      ),
    );
  }
}

/// Sort-key picker styled as a pill, matching the _IconPill controls.
class _SortMenu extends ConsumerWidget {
  const _SortMenu({required this.sort});
  final SortState sort;
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final lyc = context.lyc;
    return PopupMenuButton<SortKey>(
      tooltip: 'Sort',
      initialValue: sort.key,
      onSelected: (key) =>
          ref.read(sortControllerProvider.notifier).setKey(key),
      itemBuilder: (context) => [
        for (final key in SortKey.values)
          PopupMenuItem(
            value: key,
            child: Row(
              children: [
                Icon(
                  key == sort.key ? Icons.check_rounded : Icons.check,
                  size: 18,
                  color: key == sort.key ? lyc.brass : Colors.transparent,
                ),
                const SizedBox(width: 8),
                Text(key.label),
              ],
            ),
          ),
      ],
      child: Container(
        height: 40,
        padding: const EdgeInsets.symmetric(horizontal: 14),
        decoration: BoxDecoration(
          borderRadius: BorderRadius.circular(LycRadii.pill),
          border: Border.all(color: lyc.borderStrong),
        ),
        child: Row(
          children: [
            Icon(Icons.sort_rounded, size: 16, color: lyc.muted),
            const SizedBox(width: 8),
            Text(
              sort.key.label,
              style: TextStyle(
                fontSize: 13,
                fontWeight: FontWeight.w600,
                color: lyc.text,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _Shelf extends ConsumerWidget {
  const _Shelf({required this.books, required this.grid});
  final List<Book> books;
  final bool grid;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final sort = ref.watch(sortControllerProvider);
    final pinId = pinnedBookId(books);
    final items = buildShelf(books, sort, pinBookId: pinId);
    final listBooks = sortBooks(books, sort, pinBookId: pinId);
    return CustomScrollView(
      physics: const AlwaysScrollableScrollPhysics(),
      slivers: [
        SliverPadding(
          padding: const EdgeInsets.fromLTRB(20, 24, 20, 8),
          sliver: SliverToBoxAdapter(child: _Header(books: books)),
        ),
        if (grid)
          SliverPadding(
            padding: const EdgeInsets.fromLTRB(16, 12, 16, 40),
            sliver: SliverGrid(
              gridDelegate: _coverGridDelegate(context),
              delegate: SliverChildBuilderDelegate((context, i) {
                final item = items[i];
                return switch (item) {
                  BookItem(:final book) => BookCard(
                    book: book,
                    pinned: book.id == pinId,
                  ),
                  SeriesItem(:final series) => SeriesTile(
                    series: series,
                    // pinId already resolves to the volume to continue, so
                    // the chip never reopens a book just finished (LYCM-108).
                    continueBookId:
                        pinId != null &&
                            series.members.any((m) => m.id == pinId)
                        ? pinId
                        : null,
                  ),
                };
              }, childCount: items.length),
            ),
          )
        else
          SliverPadding(
            padding: const EdgeInsets.fromLTRB(20, 8, 20, 40),
            sliver: SliverList.separated(
              itemCount: listBooks.length,
              itemBuilder: (context, i) => BookListTile(book: listBooks[i]),
              separatorBuilder: (context, _) =>
                  Divider(height: 1, color: context.lyc.border),
            ),
          ),
      ],
    );
  }
}

/// The first thing a fresh install sees (LYCM-103).
///
/// The router only sends people to the front door once a server has told it they
/// are signed out — an app with no address has asked nobody, so this screen, not
/// `/sign-in`, is where onboarding actually starts. It therefore has to offer
/// the same one action: scan the invite, which names the library and unlocks it
/// in a single gesture. Typing an address stays behind a tap for the cases a QR
/// can't cover — a LAN box, a dev server, a bare key or pairing code.
class _ConnectPrompt extends ConsumerStatefulWidget {
  @override
  ConsumerState<_ConnectPrompt> createState() => _ConnectPromptState();
}

class _ConnectPromptState extends ConsumerState<_ConnectPrompt> {
  bool _scanning = false;
  bool _manual = false;
  String? _note;

  Future<void> _scan() async {
    if (_scanning) return;
    setState(() {
      _scanning = true;
      _note = null;
    });
    try {
      final result = await scanAndOnboard(context, ref);
      if (!mounted || result == null) return;
      setState(() {
        _note = switch (result) {
          // Signed in: the shelf is already reloading behind this card, which is
          // about to stop existing.
          Onboarded() => null,
          NeedsServerAddress() =>
            "That code doesn't say which library it belongs to. Enter the "
                'address below, then sign in with the key.',
          ServerUnreachable(address: final a) =>
            "Couldn't reach $a. Check this phone's connection, and that the "
                'address in the invite is one it can see.',
          InviteRejected() =>
            "That key didn't work — it may be spent, expired, or already used. "
                'Ask for a fresh one.',
          InviteThrottled() =>
            'Too many tries. Wait a minute, then scan again.',
        };
        if (result is NeedsServerAddress) _manual = true;
      });
    } finally {
      if (mounted) setState(() => _scanning = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    final note = _note;

    return Center(
      child: SingleChildScrollView(
        padding: const EdgeInsets.all(24),
        child: Container(
          constraints: const BoxConstraints(maxWidth: 420),
          padding: const EdgeInsets.all(22),
          decoration: BoxDecoration(
            color: lyc.surface,
            borderRadius: BorderRadius.circular(LycRadii.card),
            border: Border.all(color: lyc.border),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            mainAxisSize: MainAxisSize.min,
            children: [
              Text(
                'Connect to your library',
                style: Theme.of(context).textTheme.titleLarge,
              ),
              const SizedBox(height: 6),
              Text(
                'Scan the invite you were sent — it points Lyceum at your '
                'server and signs this device in.',
                style: TextStyle(fontSize: 13.5, color: lyc.muted, height: 1.4),
              ),
              const SizedBox(height: 18),
              ScanInviteButton(
                onPressed: _scanning ? null : _scan,
                busy: _scanning,
              ),
              if (note != null) ...[
                const SizedBox(height: 14),
                Text(
                  note,
                  style: TextStyle(
                    fontSize: 12.5,
                    height: 1.5,
                    color: lyc.error,
                  ),
                ),
              ],
              const SizedBox(height: 6),
              Center(
                child: TextButton(
                  onPressed: () => setState(() => _manual = !_manual),
                  child: Text(
                    _manual
                        ? 'Hide server address'
                        : 'Enter a server address instead',
                    style: TextStyle(fontSize: 12, color: lyc.dim),
                  ),
                ),
              ),
              if (_manual) ...[
                const SizedBox(height: 6),
                ServerSettings(
                  onSaved: () =>
                      ref.read(libraryControllerProvider.notifier).refresh(),
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

class _EmptyShelf extends StatelessWidget {
  const _EmptyShelf();
  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return ListView(
      children: [
        const SizedBox(height: 120),
        Icon(Icons.menu_book_outlined, size: 48, color: lyc.dim),
        const SizedBox(height: 16),
        Center(
          child: Text(
            'No books yet',
            style: Theme.of(context).textTheme.titleLarge,
          ),
        ),
        const SizedBox(height: 6),
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 40),
          child: Text(
            'Books appear here once your server ingests them.',
            textAlign: TextAlign.center,
            style: TextStyle(fontSize: 13.5, color: lyc.muted, height: 1.4),
          ),
        ),
      ],
    );
  }
}

class _ErrorShelf extends StatelessWidget {
  const _ErrorShelf({required this.message, required this.onRetry});
  final String message;
  final VoidCallback onRetry;
  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return ListView(
      padding: const EdgeInsets.all(24),
      children: [
        const SizedBox(height: 80),
        Center(
          child: Container(
            constraints: const BoxConstraints(maxWidth: 380),
            padding: const EdgeInsets.all(20),
            decoration: BoxDecoration(
              color: lyc.surface,
              borderRadius: BorderRadius.circular(LycRadii.card),
              border: Border.all(color: lyc.border),
            ),
            child: Column(
              children: [
                Icon(Icons.cloud_off_outlined, size: 32, color: lyc.error),
                const SizedBox(height: 12),
                Text(
                  "Can't reach the library",
                  style: Theme.of(context).textTheme.titleMedium,
                ),
                const SizedBox(height: 6),
                Text(
                  message,
                  textAlign: TextAlign.center,
                  maxLines: 3,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(fontSize: 12.5, color: lyc.muted),
                ),
                const SizedBox(height: 16),
                FilledButton(
                  onPressed: onRetry,
                  child: const Text('Try again'),
                ),
              ],
            ),
          ),
        ),
      ],
    );
  }
}

class _LoadingShelf extends StatefulWidget {
  const _LoadingShelf();
  @override
  State<_LoadingShelf> createState() => _LoadingShelfState();
}

class _LoadingShelfState extends State<_LoadingShelf>
    with SingleTickerProviderStateMixin {
  late final AnimationController _c = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 1100),
  )..repeat(reverse: true);

  @override
  void dispose() {
    _c.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return GridView.builder(
      padding: const EdgeInsets.fromLTRB(16, 28, 16, 40),
      // Same delegate as the real grid (LYCM-60) so load→content doesn't jump.
      gridDelegate: _coverGridDelegate(context),
      itemCount: 6,
      itemBuilder: (context, i) => FadeTransition(
        opacity: Tween(begin: 0.35, end: 0.7).animate(_c),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            AspectRatio(
              aspectRatio: 366 / 600,
              child: DecoratedBox(
                decoration: BoxDecoration(
                  color: lyc.surfaceRaised,
                  borderRadius: BorderRadius.circular(LycRadii.cover),
                ),
              ),
            ),
            const SizedBox(height: 10),
            Container(height: 11, width: 110, color: lyc.surfaceRaised),
            const SizedBox(height: 6),
            Container(height: 9, width: 70, color: lyc.surfaceRaised),
          ],
        ),
      ),
    );
  }
}

/// Delegate for the cover grid (LYCM-60). Targets ~220dp tiles (2-up on phones,
/// more on tablets), then derives childAspectRatio from the *actual* tile width
/// so the 366/600 cover fills the cell and the title/author footer stays a fixed
/// height — no big inter-row gaps on wide screens, no clipping on narrow ones.
/// The footer term tracks the system text scale so large-font users don't clip.
SliverGridDelegateWithMaxCrossAxisExtent _coverGridDelegate(
  BuildContext context,
) {
  const hPadding = 16.0;
  const spacing = 16.0;
  const maxExtent = 220.0;
  const coverAspect = 366 / 600;

  final width = MediaQuery.sizeOf(context).width;
  final avail = width - hPadding * 2;
  // Mirror the delegate's own column math so our ratio matches the real tileW.
  final cols = (avail / (maxExtent + spacing)).ceil().clamp(1, 999);
  final tileW = (avail - spacing * (cols - 1)) / cols;

  // Footer beneath the cover: fixed gaps (SizedBox 10 + 2) plus the title
  // (2 lines @ ~13px) and author (~11.5px) rows, which scale with system text.
  final textScale = MediaQuery.textScalerOf(context).scale(1);
  final footer = 12 + (32.5 + 14) * textScale + 4; // +4 safety buffer
  final tileH = tileW / coverAspect + footer;

  return SliverGridDelegateWithMaxCrossAxisExtent(
    maxCrossAxisExtent: maxExtent,
    mainAxisSpacing: 16,
    crossAxisSpacing: spacing,
    childAspectRatio: tileW / tileH,
  );
}
