// Package capacity holds the tier rules.
//
// It is pure on purpose: no database, no context, no clock, no IO. Everything
// a decision needs is passed in, which is what makes the rules cheap to test
// and impossible to accidentally scatter across resolvers.
//
// The four rules these functions must satisfy are in the README. Read them there,
// not here.
package capacity

import "errors"

type Tier string

const (
	Pink  Tier = "PINK"
	Blue  Tier = "BLUE"
	Green Tier = "GREEN"
)

// Tiers lists every tier, closest first.
func Tiers() []Tier { return []Tier{Pink, Blue, Green} }

// Caps is configuration, loaded at startup. Raising a cap must never require
// a code change in the enforcement path.
type Caps struct {
	Budget  int
	PerTier map[Tier]int
}

// Counts is a snapshot of one user's active contacts, keyed by tier.
type Counts map[Tier]int

// Total is the number of seats currently spent across every tier.
func (c Counts) Total() int {
	n := 0
	for _, t := range Tiers() {
		n += c[t]
	}
	return n
}

var (
	// ErrBudgetFull means the shared budget is spent, regardless of sub-caps.
	ErrBudgetFull = errors.New("capacity: shared budget is full")
	// ErrTierFull means the destination tier is full, even though the budget has room.
	ErrTierFull = errors.New("capacity: tier is full")
)

// CanSend reports whether a user holding these counts may send a new request.
// A pending request creates no contact and spends no seat.
func CanSend(caps Caps, have Counts) error {
	if have.Total() >= caps.Budget {
		return ErrBudgetFull
	}
	return nil
}

// CanAdd reports whether a new contact may be added to tier t.
// Called for both sides of an accept.
func CanAdd(caps Caps, have Counts, t Tier) error {
	if have.Total() >= caps.Budget {
		return ErrBudgetFull
	}
	if have[t] >= caps.PerTier[t] {
		return ErrTierFull
	}
	return nil
}

// CanMove reports whether an existing contact may be re-filed from one tier
// to another. The contact already occupies a seat.
func CanMove(caps Caps, have Counts, from, to Tier) error {
	if from == to {
		return nil
	}
	if have[to] >= caps.PerTier[to] {
		return ErrTierFull
	}
	return nil
}
