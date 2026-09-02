package capacity_test

import (
	"errors"
	"testing"

	"github.com/tktaofik/capacity-takehome/api/internal/capacity"
)

// testCaps mirrors the README defaults: sub-caps sum to 9, budget is 8.
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

// Rule 1 - the shared budget binds before the sub-cap.
// 3 in Blue and 5 in Green is 8 of 8, so Pink is unreachable even though
// Pink is empty and its cap is 1.
func TestBudgetBindsBeforeSubCap(t *testing.T) {
	caps := testCaps()
	have := capacity.Counts{capacity.Blue: 3, capacity.Green: 5}

	err := capacity.CanAdd(caps, have, capacity.Pink)
	if !errors.Is(err, capacity.ErrBudgetFull) {
		t.Fatalf("CanAdd(Pink) = %v, want ErrBudgetFull", err)
	}
}

// Rule 1b - a full tier fails even when the budget has room.
func TestTierFullWithBudgetRemaining(t *testing.T) {
	caps := testCaps()
	have := capacity.Counts{capacity.Blue: 3, capacity.Green: 2}

	err := capacity.CanAdd(caps, have, capacity.Blue)
	if !errors.Is(err, capacity.ErrTierFull) {
		t.Fatalf("CanAdd(Blue) = %v, want ErrTierFull", err)
	}
}

// Rule 2 - a pending request spends no seat, so sending is a budget question
// only, and one free seat permits any number of outstanding requests.
func TestSendChecksBudgetOnly(t *testing.T) {
	caps := testCaps()

	oneFree := capacity.Counts{capacity.Green: 7}
	if err := capacity.CanSend(caps, oneFree); err != nil {
		t.Fatalf("CanSend with one free seat = %v, want nil", err)
	}

	full := capacity.Counts{capacity.Blue: 3, capacity.Green: 5}
	if err := capacity.CanSend(caps, full); !errors.Is(err, capacity.ErrBudgetFull) {
		t.Fatalf("CanSend at budget = %v, want ErrBudgetFull", err)
	}

	tierFullBudgetOpen := capacity.Counts{capacity.Pink: 1}
	if err := capacity.CanSend(caps, tierFullBudgetOpen); err != nil {
		t.Fatalf("CanSend with tier full but budget open = %v, want nil", err)
	}
}

// Rule 3 - re-filing checks the destination sub-cap and never the budget,
// because the contact is already inside the budget.
func TestMoveIgnoresBudget(t *testing.T) {
	caps := testCaps()
	have := capacity.Counts{capacity.Blue: 3, capacity.Green: 5}

	err := capacity.CanMove(caps, have, capacity.Blue, capacity.Pink)
	if err != nil {
		t.Fatalf("CanMove(Blue→Pink) at full budget = %v, want nil", err)
	}

	err = capacity.CanMove(caps, have, capacity.Green, capacity.Blue)
	if !errors.Is(err, capacity.ErrTierFull) {
		t.Fatalf("CanMove(Green→Blue) = %v, want ErrTierFull", err)
	}
}

// used may legally exceed cap (a lowered cap, a merge). Nothing may assume
// used <= cap, and an over-budget user must fail closed rather than panic.
func TestOverBudgetIsHandled(t *testing.T) {
	caps := testCaps()
	have := capacity.Counts{capacity.Pink: 2, capacity.Blue: 2, capacity.Green: 5}

	if err := capacity.CanAdd(caps, have, capacity.Green); !errors.Is(err, capacity.ErrBudgetFull) {
		t.Fatalf("CanAdd over budget = %v, want ErrBudgetFull", err)
	}
	if err := capacity.CanSend(caps, have); !errors.Is(err, capacity.ErrBudgetFull) {
		t.Fatalf("CanSend over budget = %v, want ErrBudgetFull", err)
	}

	if err := capacity.CanMove(caps, have, capacity.Green, capacity.Blue); err != nil {
		t.Fatalf("CanMove over budget to tier with room = %v, want nil", err)
	}
	if err := capacity.CanMove(caps, have, capacity.Green, capacity.Pink); !errors.Is(err, capacity.ErrTierFull) {
		t.Fatalf("CanMove to over-full tier = %v, want ErrTierFull", err)
	}
}
