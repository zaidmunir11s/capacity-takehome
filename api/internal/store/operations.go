package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/tktaofik/capacity-takehome/api/internal/capacity"
)

func (s *Store) SendRequest(ctx context.Context, caps capacity.Caps, fromID, toID bson.ObjectID, tier capacity.Tier) (*Request, error) {
	if fromID == toID {
		return nil, ErrSelfRequest
	}
	if err := s.Users.FindOne(ctx, bson.M{"_id": toID}).Err(); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	if err := s.Contacts.FindOne(ctx, bson.M{"ownerId": fromID, "otherId": toID}).Err(); err == nil {
		return nil, ErrAlreadyConnected
	} else if !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, err
	}

	counts, err := s.CountsFor(ctx, fromID)
	if err != nil {
		return nil, err
	}
	if err := capacity.CanSend(caps, counts); err != nil {
		return nil, err
	}

	now := time.Now()
	req := Request{
		FromID:    fromID,
		ToID:      toID,
		Tier:      tier,
		Status:    RequestPending,
		CreatedAt: now,
	}
	res, err := s.Requests.InsertOne(ctx, req)
	if err != nil {
		return nil, err
	}
	req.ID = res.InsertedID.(bson.ObjectID)
	return &req, nil
}

func (s *Store) AcceptRequest(ctx context.Context, caps capacity.Caps, recipientID, requestID bson.ObjectID) (*Contact, error) {
	var req Request
	if err := s.Requests.FindOne(ctx, bson.M{"_id": requestID}).Decode(&req); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrRequestNotFound
		}
		return nil, err
	}
	if req.ToID != recipientID {
		return nil, ErrNotRecipient
	}
	if req.Status != RequestPending {
		return nil, ErrRequestNotPending
	}

	session, err := s.Client.StartSession()
	if err != nil {
		return nil, err
	}
	defer session.EndSession(ctx)

	result, err := session.WithTransaction(ctx, func(ctx context.Context) (any, error) {
		if err := s.tryReserveSeat(ctx, req.FromID, req.Tier, caps); err != nil {
			return nil, &AcceptCapacityError{UserID: req.FromID, Err: err}
		}
		if err := s.tryReserveSeat(ctx, req.ToID, req.Tier, caps); err != nil {
			return nil, &AcceptCapacityError{UserID: req.ToID, Err: err}
		}

		now := time.Now()
		fromContact := Contact{
			OwnerID:   req.FromID,
			OtherID:   req.ToID,
			Tier:      req.Tier,
			CreatedAt: now,
		}
		toContact := Contact{
			OwnerID:   req.ToID,
			OtherID:   req.FromID,
			Tier:      req.Tier,
			CreatedAt: now,
		}
		if _, err := s.Contacts.InsertOne(ctx, fromContact); err != nil {
			return nil, err
		}
		res, err := s.Contacts.InsertOne(ctx, toContact)
		if err != nil {
			return nil, err
		}
		toContact.ID = res.InsertedID.(bson.ObjectID)

		update := s.Requests.FindOneAndUpdate(ctx,
			bson.M{"_id": req.ID, "status": RequestPending},
			bson.M{"$set": bson.M{"status": RequestAccepted}},
		)
		if update.Err() != nil {
			if errors.Is(update.Err(), mongo.ErrNoDocuments) {
				return nil, ErrRequestNotPending
			}
			return nil, update.Err()
		}

		return &toContact, nil
	})
	if err != nil {
		return nil, err
	}
	contact, ok := result.(*Contact)
	if !ok {
		return nil, fmt.Errorf("accept request: unexpected result type %T", result)
	}
	return contact, nil
}

func (s *Store) DeclineRequest(ctx context.Context, recipientID, requestID bson.ObjectID) (*Request, error) {
	res := s.Requests.FindOneAndUpdate(ctx,
		bson.M{"_id": requestID, "toId": recipientID, "status": RequestPending},
		bson.M{"$set": bson.M{"status": RequestDeclined}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	)
	if res.Err() != nil {
		if errors.Is(res.Err(), mongo.ErrNoDocuments) {
			var req Request
			err := s.Requests.FindOne(ctx, bson.M{"_id": requestID}).Decode(&req)
			if errors.Is(err, mongo.ErrNoDocuments) {
				return nil, ErrRequestNotFound
			}
			if err != nil {
				return nil, err
			}
			if req.ToID != recipientID {
				return nil, ErrNotRecipient
			}
			return nil, ErrRequestNotPending
		}
		return nil, res.Err()
	}
	var req Request
	if err := res.Decode(&req); err != nil {
		return nil, err
	}
	return &req, nil
}

func (s *Store) MoveContact(ctx context.Context, caps capacity.Caps, ownerID, contactID bson.ObjectID, to capacity.Tier) (*Contact, error) {
	var contact Contact
	if err := s.Contacts.FindOne(ctx, bson.M{"_id": contactID, "ownerId": ownerID}).Decode(&contact); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrContactNotFound
		}
		return nil, err
	}
	if contact.Tier == to {
		return &contact, nil
	}

	session, err := s.Client.StartSession()
	if err != nil {
		return nil, err
	}
	defer session.EndSession(ctx)

	result, err := session.WithTransaction(ctx, func(ctx context.Context) (any, error) {
		if err := s.tryMoveSeat(ctx, ownerID, contact.Tier, to, caps); err != nil {
			return nil, err
		}
		if err := s.tryMoveSeat(ctx, contact.OtherID, contact.Tier, to, caps); err != nil {
			return nil, err
		}

		_, err := s.Contacts.UpdateOne(ctx,
			bson.M{"ownerId": ownerID, "otherId": contact.OtherID},
			bson.M{"$set": bson.M{"tier": to}},
		)
		if err != nil {
			return nil, err
		}
		_, err = s.Contacts.UpdateOne(ctx,
			bson.M{"ownerId": contact.OtherID, "otherId": ownerID},
			bson.M{"$set": bson.M{"tier": to}},
		)
		if err != nil {
			return nil, err
		}

		contact.Tier = to
		return &contact, nil
	})
	if err != nil {
		return nil, err
	}
	moved, ok := result.(*Contact)
	if !ok {
		return nil, fmt.Errorf("move contact: unexpected result type %T", result)
	}
	return moved, nil
}

func (s *Store) RemoveContact(ctx context.Context, ownerID, contactID bson.ObjectID) error {
	var contact Contact
	if err := s.Contacts.FindOne(ctx, bson.M{"_id": contactID, "ownerId": ownerID}).Decode(&contact); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return ErrContactNotFound
		}
		return err
	}

	session, err := s.Client.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(ctx)

	_, err = session.WithTransaction(ctx, func(ctx context.Context) (any, error) {
		if _, err := s.Contacts.DeleteOne(ctx, bson.M{"ownerId": ownerID, "otherId": contact.OtherID}); err != nil {
			return nil, err
		}
		if _, err := s.Contacts.DeleteOne(ctx, bson.M{"ownerId": contact.OtherID, "otherId": ownerID}); err != nil {
			return nil, err
		}
		if err := s.releaseSeat(ctx, ownerID, contact.Tier); err != nil {
			return nil, err
		}
		if err := s.releaseSeat(ctx, contact.OtherID, contact.Tier); err != nil {
			return nil, err
		}
		return nil, nil
	})
	return err
}
