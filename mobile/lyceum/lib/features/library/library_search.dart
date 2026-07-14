import 'package:flutter/material.dart';

import '../../api/models.dart';
import '../../theme/lyceum_colors.dart';
import '../../widgets/cover_image.dart';
import 'shelf.dart';

/// Full-screen search that opens above the shelf on demand (LYCM-63), the mobile
/// counterpart to the web overlay. Filters by title/author/series as you type.
/// Series are not rolled up here — every matching volume is listed directly.
class LibrarySearchDelegate extends SearchDelegate<void> {
  LibrarySearchDelegate({
    required this.books,
    required this.sort,
    required this.coverUrlOf,
    required this.onOpen,
  }) : super(searchFieldLabel: 'Search by title or author');

  final List<Book> books;
  final SortState sort;
  final String Function(int id) coverUrlOf;
  final void Function(int id) onOpen;

  List<Book> _matches() =>
      sortBooks(books.where((b) => matchesQuery(b, query)).toList(), sort);

  @override
  List<Widget> buildActions(BuildContext context) => [
    if (query.isNotEmpty)
      IconButton(
        icon: const Icon(Icons.close_rounded),
        onPressed: () => query = '',
      ),
  ];

  @override
  Widget buildLeading(BuildContext context) => IconButton(
    icon: const Icon(Icons.arrow_back_rounded),
    onPressed: () => close(context, null),
  );

  @override
  Widget buildSuggestions(BuildContext context) => _results(context);

  @override
  Widget buildResults(BuildContext context) => _results(context);

  Widget _results(BuildContext context) {
    final lyc = context.lyc;
    if (query.trim().isEmpty) {
      return const SizedBox.shrink();
    }
    final matches = _matches();
    if (matches.isEmpty) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(32),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(Icons.search_off_rounded, size: 40, color: lyc.dim),
              const SizedBox(height: 12),
              Text(
                'No matches',
                style: Theme.of(context).textTheme.titleMedium,
              ),
              const SizedBox(height: 4),
              Text(
                'Nothing on the shelf matches “${query.trim()}”.',
                textAlign: TextAlign.center,
                style: TextStyle(fontSize: 13, color: lyc.muted),
              ),
            ],
          ),
        ),
      );
    }
    return ListView.separated(
      padding: const EdgeInsets.symmetric(vertical: 8),
      itemCount: matches.length,
      separatorBuilder: (_, _) => Divider(height: 1, color: lyc.border),
      itemBuilder: (context, i) {
        final b = matches[i];
        return ListTile(
          onTap: () {
            onOpen(b.id);
            close(context, null);
          },
          leading: ClipRRect(
            borderRadius: BorderRadius.circular(4),
            child: SizedBox(
              width: 34,
              height: 50,
              child: b.hasCover
                  ? CoverImage(
                      url: coverUrlOf(b.id),
                      fallback: ColoredBox(color: lyc.surfaceRaised),
                    )
                  : ColoredBox(color: lyc.surfaceRaised),
            ),
          ),
          title: Text(
            b.title,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: TextStyle(fontWeight: FontWeight.w700, color: lyc.text),
          ),
          subtitle: Text(
            b.series != null && b.series!.isNotEmpty
                ? '${b.author} · ${b.series}'
                : b.author,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: TextStyle(color: lyc.dim),
          ),
        );
      },
    );
  }
}
