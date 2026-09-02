package store

import (
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/tktaofik/capacity-takehome/api/internal/capacity"
)

var (
	ErrUserNotFound     = errors.New("user not found")
	ErrContactNotFound  = errors.New("contact not found")
	ErrRequestNotFound  = errors.New("request not found")
	ErrRequestNotPending = errors.New("request is not pending")
	ErrNotRecipient     = errors.New("only the recipient can respond to this request")
	ErrSelfRequest      = errors.New("cannot send a request to yourself")
	ErrAlreadyConnected = errors.New("already connected with this user")
)

// AcceptCapacityError records which side of an accept failed capacity.
type AcceptCapacityError struct {
	UserID bson.ObjectID
	Err    error
}

func (e *AcceptCapacityError) Error() string {
	return fmt.Sprintf("accept capacity for %s: %v", e.UserID.Hex(), e.Err)
}

func (e *AcceptCapacityError) Unwrap() error { return e.Err }

func IsCapacityError(err error) bool {
	return errors.Is(err, capacity.ErrBudgetFull) || errors.Is(err, capacity.ErrTierFull)
}
