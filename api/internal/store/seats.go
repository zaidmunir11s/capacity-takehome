package store

import (
	"context"
	"errors"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/tktaofik/capacity-takehome/api/internal/capacity"
)

// userSeats mirrors active contact counts for atomic capacity enforcement.
//
// Concurrency: accept must not use read → check (CanAdd) → write (insert contact).
// Two goroutines can both read "7 of 8 used" and both pass before either writes,
// landing at 9 of 8. Instead, tryReserveSeat uses a single FindOneAndUpdate whose
// filter embeds budget and tier caps ($expr) and whose update $inc's the tier count.
// MongoDB applies that filter+update atomically on the userSeats document, so only
// one concurrent reservation can succeed for the last seat. Accept wraps reservation,
// contact inserts, and request status in a transaction so a failed step rolls back
// any seat increments from the same attempt.
type userSeats struct {
	ID    bson.ObjectID `bson:"_id"`
	Pink  int           `bson:"pink"`
	Blue  int           `bson:"blue"`
	Green int           `bson:"green"`
}

func tierField(t capacity.Tier) string {
	switch t {
	case capacity.Pink:
		return "pink"
	case capacity.Blue:
		return "blue"
	case capacity.Green:
		return "green"
	default:
		return "pink"
	}
}

func tierExprField(t capacity.Tier) string {
	return "$" + tierField(t)
}

func (s *Store) ensureUserSeats(ctx context.Context, userID bson.ObjectID) error {
	_, err := s.UserSeats.UpdateOne(ctx,
		bson.M{"_id": userID},
		bson.M{"$setOnInsert": bson.M{"pink": 0, "blue": 0, "green": 0}},
		options.UpdateOne().SetUpsert(true),
	)
	return err
}

func (s *Store) seatCounts(ctx context.Context, userID bson.ObjectID) (capacity.Counts, error) {
	var doc userSeats
	err := s.UserSeats.FindOne(ctx, bson.M{"_id": userID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return capacity.Counts{}, nil
	}
	if err != nil {
		return nil, err
	}
	return capacity.Counts{
		capacity.Pink:  doc.Pink,
		capacity.Blue:  doc.Blue,
		capacity.Green: doc.Green,
	}, nil
}

func addSeatFilter(userID bson.ObjectID, tier capacity.Tier, caps capacity.Caps) bson.M {
	return bson.M{
		"_id": userID,
		"$expr": bson.M{
			"$and": bson.A{
				bson.M{"$lt": bson.A{
					bson.M{"$add": bson.A{
						bson.M{"$ifNull": bson.A{"$pink", 0}},
						bson.M{"$ifNull": bson.A{"$blue", 0}},
						bson.M{"$ifNull": bson.A{"$green", 0}},
					}},
					caps.Budget,
				}},
				bson.M{"$lt": bson.A{
					bson.M{"$ifNull": bson.A{tierExprField(tier), 0}},
					caps.PerTier[tier],
				}},
			},
		},
	}
}

func moveSeatFilter(userID bson.ObjectID, to capacity.Tier, caps capacity.Caps) bson.M {
	return bson.M{
		"_id": userID,
		"$expr": bson.M{
			"$lt": bson.A{
				bson.M{"$ifNull": bson.A{tierExprField(to), 0}},
				caps.PerTier[to],
			},
		},
	}
}

func (s *Store) tryReserveSeat(ctx context.Context, userID bson.ObjectID, tier capacity.Tier, caps capacity.Caps) error {
	if err := s.ensureUserSeats(ctx, userID); err != nil {
		return err
	}
	err := s.UserSeats.FindOneAndUpdate(ctx,
		addSeatFilter(userID, tier, caps),
		bson.M{"$inc": bson.M{tierField(tier): 1}},
	).Err()
	if err == nil {
		return nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return err
	}
	counts, err := s.seatCounts(ctx, userID)
	if err != nil {
		return err
	}
	return capacity.CanAdd(caps, counts, tier)
}

func (s *Store) tryMoveSeat(ctx context.Context, userID bson.ObjectID, from, to capacity.Tier, caps capacity.Caps) error {
	if from == to {
		return nil
	}
	if err := s.ensureUserSeats(ctx, userID); err != nil {
		return err
	}
	err := s.UserSeats.FindOneAndUpdate(ctx,
		moveSeatFilter(userID, to, caps),
		bson.M{"$inc": bson.M{
			tierField(from): -1,
			tierField(to):   1,
		}},
	).Err()
	if err == nil {
		return nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return err
	}
	counts, err := s.seatCounts(ctx, userID)
	if err != nil {
		return err
	}
	return capacity.CanMove(caps, counts, from, to)
}

func (s *Store) releaseSeat(ctx context.Context, userID bson.ObjectID, tier capacity.Tier) error {
	if err := s.ensureUserSeats(ctx, userID); err != nil {
		return err
	}
	_, err := s.UserSeats.UpdateOne(ctx,
		bson.M{"_id": userID},
		bson.M{"$inc": bson.M{tierField(tier): -1}},
	)
	return err
}
