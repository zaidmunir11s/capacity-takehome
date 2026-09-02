package graph

import (
	"context"
	"errors"
	"fmt"

	"github.com/tktaofik/capacity-takehome/api/graph/model"
	"github.com/tktaofik/capacity-takehome/api/internal/capacity"
	"github.com/tktaofik/capacity-takehome/api/internal/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func toCapacityTier(t model.Tier) capacity.Tier {
	return capacity.Tier(t)
}

func toModelTier(t capacity.Tier) model.Tier {
	return model.Tier(t)
}

func toModelRequestStatus(s store.RequestStatus) model.RequestStatus {
	return model.RequestStatus(s)
}

func (r *Resolver) userByID(ctx context.Context, id bson.ObjectID) (*model.User, error) {
	var u store.User
	if err := r.Store.Users.FindOne(ctx, bson.M{"_id": id}).Decode(&u); err != nil {
		return nil, fmt.Errorf("user %s: %w", id.Hex(), err)
	}
	return &model.User{ID: u.ID.Hex(), Name: u.Name}, nil
}

func (r *Resolver) contactToModel(ctx context.Context, c *store.Contact) (*model.Contact, error) {
	user, err := r.userByID(ctx, c.OtherID)
	if err != nil {
		return nil, err
	}
	return &model.Contact{
		ID:        c.ID.Hex(),
		User:      user,
		Tier:      toModelTier(c.Tier),
		CreatedAt: c.CreatedAt,
	}, nil
}

func (r *Resolver) requestToModel(ctx context.Context, req *store.Request) (*model.Request, error) {
	from, err := r.userByID(ctx, req.FromID)
	if err != nil {
		return nil, err
	}
	to, err := r.userByID(ctx, req.ToID)
	if err != nil {
		return nil, err
	}
	return &model.Request{
		ID:        req.ID.Hex(),
		From:      from,
		To:        to,
		Tier:      toModelTier(req.Tier),
		Status:    toModelRequestStatus(req.Status),
		CreatedAt: req.CreatedAt,
	}, nil
}

func mapCapacityError(err error, callerID bson.ObjectID) error {
	var acceptErr *store.AcceptCapacityError
	if errors.As(err, &acceptErr) {
		subject := "Their"
		if acceptErr.UserID == callerID {
			subject = "Your"
		}
		return errors.New(capacityMessage(acceptErr.Err, subject))
	}
	if errors.Is(err, capacity.ErrBudgetFull) {
		return errors.New(capacityMessage(err, "Your"))
	}
	if errors.Is(err, capacity.ErrTierFull) {
		return errors.New(capacityMessage(err, "Your"))
	}
	return err
}

func capacityMessage(err error, subject string) string {
	switch {
	case errors.Is(err, capacity.ErrBudgetFull):
		return fmt.Sprintf("%s contact list is full.", subject)
	case errors.Is(err, capacity.ErrTierFull):
		return fmt.Sprintf("%s tier is full.", subject)
	default:
		return err.Error()
	}
}

func parseObjectID(id string) (bson.ObjectID, error) {
	return bson.ObjectIDFromHex(id)
}
