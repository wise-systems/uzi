package issuetracker

import "errors"

// pageSize is the $top of every list request.
const pageSize = 100

// maxItems and maxPages are sanity ceilings against a server that never ends a
// listing. Vars so tests can lower them.
var (
	maxItems = 1_000_000
	maxPages = 20_000
)

// paginate calls fetch with a growing skip until a short page. A positive limit
// stops once that many items are collected and truncates to it; zero means no
// cap. Exceeding maxItems or maxPages is an error carrying no remote content.
func paginate[T any](fetch func(skip, top int) ([]T, error), limit int) ([]T, error) {
	var all []T
	for page := 0; ; page++ {
		if page >= maxPages {
			return nil, errors.New("issuetracker: listing exceeds the page ceiling")
		}
		items, err := fetch(len(all), pageSize)
		if err != nil {
			return nil, err
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
