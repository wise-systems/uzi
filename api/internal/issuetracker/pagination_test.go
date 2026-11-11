package issuetracker

import (
	"errors"
	"testing"
)

// serve returns a fetch over total synthetic items, recording each call.
func serve(total int, calls *[][2]int) func(skip, top int) ([]int, error) {
	return func(skip, top int) ([]int, error) {
		*calls = append(*calls, [2]int{skip, top})
		var out []int
		for i := skip; i < skip+top && i < total; i++ {
			out = append(out, i)
		}
		return out, nil
	}
}

func TestPaginateCollectsAll(t *testing.T) {
	var calls [][2]int
	got, err := paginate(serve(250, &calls), 0)
	if err != nil || len(got) != 250 {
		t.Fatalf("len %d, err %v", len(got), err)
	}
	if len(calls) != 3 || calls[2] != [2]int{200, pageSize} {
		t.Fatalf("calls = %v", calls)
	}
}

func TestPaginateExactMultipleFetchesEmptyPage(t *testing.T) {
	var calls [][2]int
	got, err := paginate(serve(200, &calls), 0)
	if err != nil || len(got) != 200 || len(calls) != 3 {
		t.Fatalf("len %d, calls %d, err %v", len(got), len(calls), err)
	}
}

func TestPaginateLimit(t *testing.T) {
	var calls [][2]int
	got, err := paginate(serve(1000, &calls), 150)
	if err != nil || len(got) != 150 || len(calls) != 2 {
		t.Fatalf("len %d, calls %d, err %v", len(got), len(calls), err)
	}
}

func TestPaginateFetchError(t *testing.T) {
	want := errors.New("boom")
	_, err := paginate(func(int, int) ([]int, error) { return nil, want }, 0)
	if !errors.Is(err, want) {
		t.Fatalf("err = %v", err)
	}
}

func TestPaginateCaps(t *testing.T) {
	t.Run("items", func(t *testing.T) {
		old := maxItems
		maxItems = 250
		t.Cleanup(func() { maxItems = old })
		var calls [][2]int
		if _, err := paginate(serve(10_000, &calls), 0); err == nil {
			t.Fatal("expected item ceiling error")
		}
	})
	t.Run("pages", func(t *testing.T) {
		old := maxPages
		maxPages = 3
		t.Cleanup(func() { maxPages = old })
		var calls [][2]int
		if _, err := paginate(serve(10_000, &calls), 0); err == nil {
			t.Fatal("expected page ceiling error")
		}
		if len(calls) != 3 {
			t.Fatalf("calls = %d", len(calls))
		}
	})
}
