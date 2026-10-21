package authz

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDecisionMemoSharesOneEvaluationAndNeverCachesFailures(t *testing.T) {
	m := memoFrom(WithDecisionMemo(context.Background()))
	var calls atomic.Int32
	release := make(chan struct{})
	slow := func() (bool, error) {
		calls.Add(1)
		<-release
		return false, nil
	}
	var wg sync.WaitGroup
	results := make([]bool, 5)
	for i := range results {
		wg.Go(func() { results[i], _ = m.do("k", slow) })
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("concurrent callers evaluated %d times, want once", calls.Load())
	}
	// A denial is memoized like a grant.
	if allowed, err := m.do("k", func() (bool, error) { calls.Add(1); return true, nil }); allowed || err != nil || calls.Load() != 1 {
		t.Fatalf("memoized denial = %v, %v after %d calls", allowed, err, calls.Load())
	}

	boom := errors.New("boom")
	if _, err := m.do("e", func() (bool, error) { return false, boom }); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if allowed, err := m.do("e", func() (bool, error) { return true, nil }); !allowed || err != nil {
		t.Fatalf("a failure was memoized: %v, %v", allowed, err)
	}
	if memoFrom(context.Background()) != nil {
		t.Fatal("memo outside a request")
	}
}
