import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../api/models.dart';
import '../../api/server_store.dart';
import '../../prefs/profile.dart';
import '../../theme/lyceum_colors.dart';
import '../../widgets/brand_mark.dart';
import '../settings/server_settings.dart';
import 'book_card.dart';
import 'library_controller.dart';

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
                      onRefresh: () =>
                          ref.read(libraryControllerProvider.notifier).refresh(),
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
    final initial = ref.watch(profileInitialProvider);
    return Container(
      padding: const EdgeInsets.fromLTRB(18, 12, 14, 12),
      decoration: BoxDecoration(
        border: Border(bottom: BorderSide(color: lyc.border)),
      ),
      child: Row(
        children: [
          const BrandMark(),
          const Spacer(),
          GestureDetector(
            onTap: () => context.push('/settings'),
            child: CircleAvatar(
              radius: 18,
              backgroundColor: lyc.brassWash,
              child: Text(initial,
                  style: TextStyle(
                      color: lyc.brassBright,
                      fontWeight: FontWeight.w700,
                      fontSize: 14)),
            ),
          ),
        ],
      ),
    );
  }
}

/// Small circular icon button (used for the grid/list toggle in the header).
class _IconPill extends StatelessWidget {
  const _IconPill({required this.icon, required this.onTap});
  final IconData icon;
  final VoidCallback onTap;
  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return GestureDetector(
      onTap: onTap,
      child: Container(
        width: 40,
        height: 40,
        decoration: BoxDecoration(
          shape: BoxShape.circle,
          border: Border.all(color: lyc.borderStrong),
        ),
        child: Icon(icon, size: 18, color: lyc.muted),
      ),
    );
  }
}

class _Header extends ConsumerWidget {
  const _Header({required this.count});
  final int count;
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final lyc = context.lyc;
    final grid = ref.watch(gridViewProvider);
    return Row(
      crossAxisAlignment: CrossAxisAlignment.center,
      children: [
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text('YOUR LIBRARY',
                  style: TextStyle(
                    fontSize: 11.5,
                    fontWeight: FontWeight.w700,
                    letterSpacing: 3,
                    color: lyc.brass,
                  )),
              const SizedBox(height: 6),
              Text('All Books',
                  style: Theme.of(context).textTheme.headlineLarge),
              const SizedBox(height: 4),
              Text('$count on the shelf',
                  style: TextStyle(fontSize: 13, color: lyc.dim)),
            ],
          ),
        ),
        // Grid/list toggle sits across from the header to save top-bar space.
        _IconPill(
          icon: grid ? Icons.view_list_rounded : Icons.grid_view_rounded,
          onTap: () => ref.read(gridViewProvider.notifier).toggle(),
        ),
      ],
    );
  }
}

class _Shelf extends StatelessWidget {
  const _Shelf({required this.books, required this.grid});
  final List<Book> books;
  final bool grid;

  @override
  Widget build(BuildContext context) {
    return CustomScrollView(
      physics: const AlwaysScrollableScrollPhysics(),
      slivers: [
        SliverPadding(
          padding: const EdgeInsets.fromLTRB(20, 24, 20, 8),
          sliver: SliverToBoxAdapter(child: _Header(count: books.length)),
        ),
        if (grid)
          SliverPadding(
            padding: const EdgeInsets.fromLTRB(16, 12, 16, 40),
            sliver: SliverGrid(
              gridDelegate: _coverGridDelegate(context),
              delegate: SliverChildBuilderDelegate(
                (context, i) => BookCard(book: books[i]),
                childCount: books.length,
              ),
            ),
          )
        else
          SliverPadding(
            padding: const EdgeInsets.fromLTRB(20, 8, 20, 40),
            sliver: SliverList.separated(
              itemCount: books.length,
              itemBuilder: (context, i) => BookListTile(book: books[i]),
              separatorBuilder: (context, _) =>
                  Divider(height: 1, color: context.lyc.border),
            ),
          ),
      ],
    );
  }
}

class _ConnectPrompt extends ConsumerWidget {
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final lyc = context.lyc;
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
            crossAxisAlignment: CrossAxisAlignment.start,
            mainAxisSize: MainAxisSize.min,
            children: [
              Text('Connect to your library',
                  style: Theme.of(context).textTheme.titleLarge),
              const SizedBox(height: 6),
              Text(
                'Point Lyceum at your self-hosted server to see your shelf.',
                style: TextStyle(fontSize: 13.5, color: lyc.muted, height: 1.4),
              ),
              const SizedBox(height: 18),
              ServerSettings(
                onSaved: () =>
                    ref.read(libraryControllerProvider.notifier).refresh(),
              ),
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
            child: Text('No books yet',
                style: Theme.of(context).textTheme.titleLarge)),
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
                Text("Can't reach the library",
                    style: Theme.of(context).textTheme.titleMedium),
                const SizedBox(height: 6),
                Text(message,
                    textAlign: TextAlign.center,
                    maxLines: 3,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(fontSize: 12.5, color: lyc.muted)),
                const SizedBox(height: 16),
                FilledButton(onPressed: onRetry, child: const Text('Try again')),
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
SliverGridDelegateWithMaxCrossAxisExtent _coverGridDelegate(BuildContext context) {
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
