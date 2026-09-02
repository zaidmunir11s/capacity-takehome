package store_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/tktaofik/capacity-takehome/api/internal/capacity"
	"github.com/tktaofik/capacity-takehome/api/internal/store"
)

// Rule 4 - two accepts landing at the same moment on a user with exactly one
// free seat must not both succeed. Exactly one wins; the other fails cleanly.
//
// This one needs a real Mongo (make up), which is why it lives here and not in
// the capacity package. Read-then-write will pass a serial test and fail this
// one - that is the point of it.
func TestConcurrentAcceptsTakeOneSeat(t *testing.T) {
	st := connectStore(t)
	resetCollections(t, st)
	caps := testCaps()
	ctx := context.Background()

	recipient := insertUser(t, st, "recipient")
	fillUserToTotal(t, st, recipient, 7)

	senderA := insertUser(t, st, "sender-a")
	senderB := insertUser(t, st, "sender-b")

	reqA, err := st.SendRequest(ctx, caps, senderA, recipient, capacity.Pink)
	if err != nil {
		t.Fatal(err)
	}
	reqB, err := st.SendRequest(ctx, caps, senderB, recipient, capacity.Pink)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	results := make(chan error, 2)

	go func() {
		defer wg.Done()
		_, err := st.AcceptRequest(ctx, caps, recipient, reqA.ID)
		results <- err
	}()
	go func() {
		defer wg.Done()
		_, err := st.AcceptRequest(ctx, caps, recipient, reqB.ID)
		results <- err
	}()
	wg.Wait()
	close(results)

	var successes, failures int
	for err := range results {
		if err == nil {
			successes++
			continue
		}
		failures++
		if !errors.Is(err, capacity.ErrBudgetFull) && !errors.As(err, new(*store.AcceptCapacityError)) {
			t.Fatalf("unexpected failure: %v", err)
		}
	}
	if successes != 1 || failures != 1 {
		t.Fatalf("want exactly one success and one failure, got successes=%d failures=%d", successes, failures)
	}

	assertUserAtBudgetCap(t, st, recipient, caps.Budget)
}

// Stress Rule 4 across many rounds: with one seat left, barrage concurrent accepts;
// capacity must never exceed the budget.
func TestConcurrentAcceptsNeverExceedCapacity(t *testing.T) {
	st := connectStore(t)
	caps := testCaps()
	ctx := context.Background()

	const rounds = 20
	var violate atomic.Bool

	for round := 0; round < rounds; round++ {
		resetCollections(t, st)

		recipient := insertUser(t, st, "recipient")
		fillUserToTotal(t, st, recipient, caps.Budget-1)

		const contenders = 8
		reqs := make([]bson.ObjectID, contenders)
		for i := 0; i < contenders; i++ {
			sender := insertUser(t, st, "sender-"+string(rune('a'+i)))
			req, err := st.SendRequest(ctx, caps, sender, recipient, capacity.Pink)
			if err != nil {
				t.Fatal(err)
			}
			reqs[i] = req.ID
		}

		var wg sync.WaitGroup
		wg.Add(contenders)
		for _, reqID := range reqs {
			go func(id bson.ObjectID) {
				defer wg.Done()
				_, _ = st.AcceptRequest(ctx, caps, recipient, id)
			}(reqID)
		}
		wg.Wait()

		counts, err := st.CountsFor(ctx, recipient)
		if err != nil {
			t.Fatal(err)
		}
		if counts.Total() > caps.Budget {
			violate.Store(true)
			t.Fatalf("round %d: recipient total %d exceeds budget %d", round, counts.Total(), caps.Budget)
		}
	}

	if violate.Load() {
		t.Fatal("capacity invariant violated under concurrent accepts")
	}
}

func assertUserAtBudgetCap(t *testing.T, st *store.Store, userID bson.ObjectID, budget int) {
	t.Helper()
	counts, err := st.CountsFor(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Total() != budget {
		t.Fatalf("user total = %d, want %d", counts.Total(), budget)
	}
}
