package issuetracker

import "errors"

// pageSize is the $top of every list request.
const pageSize = 100

// maxItems, maxPages and maxTotalBytes are sanity ceilings against a server
// that never ends a listing. Vars so tests can lower them.
var (
	maxItems      = 100_000
	maxPages      = 2_000
	maxTotalBytes = 64 << 20
)

// paginate calls fetch with a growing skip until a short page. A positive limit
// stops once that many items are collected and truncates to it; zero means no
// cap. fetch returns the page and its raw byte count; the counts are summed
// across pages. Exceeding maxItems, maxPages or maxTotalBytes is an error
// carrying no remote content.
func paginate[T any](fetch func(skip, top int) ([]T, int, error), limit int) ([]T, error) {
	var all []T
	total := 0
	for page := 0; ; page++ {
		if page >= maxPages {
			return nil, errors.New("issuetracker: listing exceeds the page ceiling")
		}
		items, n, err := fetch(len(all), pageSize)
		if err != nil {
			return nil, err
		}
		total += n
		if total > maxTotalBytes {
			return nil, errors.New("issuetracker: listing exceeds the byte ceiling")
		}
		all = append(all, items...)
		if limit > 0 && len(all) >= limit {
			return all[:limit], nil
		}
		if len(all) > maxItems {
			return nil, errors.New("issuetracker: listing exceeds the item ceiling")
		}
		if len(items) < pageSize {
			return all, nil
		}
	}
}
