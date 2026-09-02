package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/tktaofik/capacity-takehome/api/internal/capacity"
	"github.com/tktaofik/capacity-takehome/api/internal/config"
	"github.com/tktaofik/capacity-takehome/api/internal/store"
)

func testCaps() capacity.Caps {
	return capacity.Caps{
		Budget: 8,
		PerTier: map[capacity.Tier]int{
			capacity.Pink:  1,
			capacity.Blue:  3,
			capacity.Green: 5,
		},
	}
}

func connectStore(t *testing.T) *store.Store {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	st, err := store.Connect(ctx, config.MongoURI())
	if err != nil {
		t.Skipf("mongo not available: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Client.Disconnect(context.Background())
	})
	return st
}

func resetCollections(t *testing.T, st *store.Store) {
	t.Helper()
	ctx := context.Background()
	if _, err := st.Contacts.DeleteMany(ctx, bson.M{}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Requests.DeleteMany(ctx, bson.M{}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UserSeats.DeleteMany(ctx, bson.M{}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Users.DeleteMany(ctx, bson.M{}); err != nil {
		t.Fatal(err)
	}
}

func insertUser(t *testing.T, st *store.Store, name string) bson.ObjectID {
	t.Helper()
	res, err := st.Users.InsertOne(context.Background(), store.User{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	return res.InsertedID.(bson.ObjectID)
}

func fillUserToTotal(t *testing.T, st *store.Store, userID bson.ObjectID, n int) {
	t.Helper()
	caps := testCaps()
	ctx := context.Background()
	tiers := []capacity.Tier{capacity.Green, capacity.Blue, capacity.Pink}
	tierCounts := make(map[capacity.Tier]int)

	for i := 0; i < n; i++ {
		var tier capacity.Tier
		found := false
		for _, candidate := range tiers {
			if tierCounts[candidate] < caps.PerTier[candidate] {
				tier = candidate
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("cannot fill user to %d within tier caps", n)
		}

		other := insertUser(t, st, userID.Hex()+"-fill-"+string(rune('a'+i)))
		req, err := st.SendRequest(ctx, caps, other, userID, tier)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.AcceptRequest(ctx, caps, userID, req.ID); err != nil {
			t.Fatal(err)
		}
		tierCounts[tier]++
	}
}

func TestSendRequestDoesNotConsumeCapacity(t *testing.T) {
	st := connectStore(t)
	resetCollections(t, st)
	caps := testCaps()

	from := insertUser(t, st, "sender")
	to := insertUser(t, st, "recipient")

	if _, err := st.SendRequest(context.Background(), caps, from, to, capacity.Blue); err != nil {
		t.Fatalf("SendRequest: %v", err)
	}

	counts, err := st.CountsFor(context.Background(), from)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Total() != 0 {
		t.Fatalf("pending request consumed capacity: total=%d", counts.Total())
	}
}

func TestAcceptCreatesBilateralContacts(t *testing.T) {
	st := connectStore(t)
	resetCollections(t, st)
	caps := testCaps()
	ctx := context.Background()

	from := insertUser(t, st, "from")
	to := insertUser(t, st, "to")

	req, err := st.SendRequest(ctx, caps, from, to, capacity.Pink)
	if err != nil {
		t.Fatal(err)
	}
	contact, err := st.AcceptRequest(ctx, caps, to, req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if contact.OwnerID != to || contact.OtherID != from || contact.Tier != capacity.Pink {
		t.Fatalf("unexpected contact: %+v", contact)
	}

	fromCounts, err := st.CountsFor(ctx, from)
	if err != nil {
		t.Fatal(err)
	}
	toCounts, err := st.CountsFor(ctx, to)
	if err != nil {
		t.Fatal(err)
	}
	if fromCounts.Total() != 1 || toCounts.Total() != 1 {
		t.Fatalf("want 1 contact each, got from=%d to=%d", fromCounts.Total(), toCounts.Total())
	}
}

func TestAcceptValidatesBothUsers(t *testing.T) {
	st := connectStore(t)
	resetCollections(t, st)
	caps := testCaps()
	ctx := context.Background()

	from := insertUser(t, st, "from")
	to := insertUser(t, st, "to")
	fillUserToTotal(t, st, from, 7)

	req, err := st.SendRequest(ctx, caps, from, to, capacity.Pink)
	if err != nil {
		t.Fatal(err)
	}

	// Fill the requester's last seat after the pending request was sent.
	extra := insertUser(t, st, "extra")
	extraReq, err := st.SendRequest(ctx, caps, extra, from, capacity.Blue)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AcceptRequest(ctx, caps, from, extraReq.ID); err != nil {
		t.Fatal(err)
	}

	_, err = st.AcceptRequest(ctx, caps, to, req.ID)
	var capErr *store.AcceptCapacityError
	if !errors.As(err, &capErr) {
		t.Fatalf("AcceptRequest = %v, want AcceptCapacityError", err)
	}
	if capErr.UserID != from {
		t.Fatalf("capacity error user = %v, want requester %v", capErr.UserID, from)
	}
	if !errors.Is(capErr.Err, capacity.ErrBudgetFull) {
		t.Fatalf("capacity error = %v, want ErrBudgetFull", capErr.Err)
	}
}

func TestDeclineRequest(t *testing.T) {
	st := connectStore(t)
	resetCollections(t, st)
	caps := testCaps()
	ctx := context.Background()

	from := insertUser(t, st, "from")
	to := insertUser(t, st, "to")

	req, err := st.SendRequest(ctx, caps, from, to, capacity.Blue)
	if err != nil {
		t.Fatal(err)
	}
	declined, err := st.DeclineRequest(ctx, to, req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if declined.Status != store.RequestDeclined {
		t.Fatalf("status = %q, want DECLINED", declined.Status)
	}

	counts, err := st.CountsFor(ctx, from)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Total() != 0 {
		t.Fatalf("decline created contacts: total=%d", counts.Total())
	}
}

func TestMoveDoesNotChangeTotalCapacity(t *testing.T) {
	st := connectStore(t)
	resetCollections(t, st)
	caps := testCaps()
	ctx := context.Background()

	from := insertUser(t, st, "from")
	to := insertUser(t, st, "to")

	req, err := st.SendRequest(ctx, caps, from, to, capacity.Green)
	if err != nil {
		t.Fatal(err)
	}
	contact, err := st.AcceptRequest(ctx, caps, to, req.ID)
	if err != nil {
		t.Fatal(err)
	}

	before, err := st.CountsFor(ctx, to)
	if err != nil {
		t.Fatal(err)
	}

	moved, err := st.MoveContact(ctx, caps, to, contact.ID, capacity.Blue)
	if err != nil {
		t.Fatal(err)
	}
	if moved.Tier != capacity.Blue {
		t.Fatalf("tier = %q, want BLUE", moved.Tier)
	}

	after, err := st.CountsFor(ctx, to)
	if err != nil {
		t.Fatal(err)
	}
	if after.Total() != before.Total() {
		t.Fatalf("move changed total capacity: before=%d after=%d", before.Total(), after.Total())
	}
	if after[capacity.Green] != before[capacity.Green]-1 || after[capacity.Blue] != before[capacity.Blue]+1 {
		t.Fatalf("move miscounted tiers: before=%v after=%v", before, after)
	}
}

func TestRemoveReleasesCapacity(t *testing.T) {
	st := connectStore(t)
	resetCollections(t, st)
	caps := testCaps()
	ctx := context.Background()

	from := insertUser(t, st, "from")
	to := insertUser(t, st, "to")

	req, err := st.SendRequest(ctx, caps, from, to, capacity.Pink)
	if err != nil {
		t.Fatal(err)
	}
	contact, err := st.AcceptRequest(ctx, caps, to, req.ID)
	if err != nil {
		t.Fatal(err)
	}

	if err := st.RemoveContact(ctx, to, contact.ID); err != nil {
		t.Fatal(err)
	}

	toCounts, err := st.CountsFor(ctx, to)
	if err != nil {
		t.Fatal(err)
	}
	fromCounts, err := st.CountsFor(ctx, from)
	if err != nil {
		t.Fatal(err)
	}
	if toCounts.Total() != 0 || fromCounts.Total() != 0 {
		t.Fatalf("remove did not release capacity: from=%v to=%v", fromCounts, toCounts)
	}
}
