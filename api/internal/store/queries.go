package store

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func (s *Store) ContactsFor(ctx context.Context, ownerID bson.ObjectID) ([]Contact, error) {
	cur, err := s.Contacts.Find(ctx, bson.M{"ownerId": ownerID}, options.Find().SetSort(bson.D{{Key: "tier", Value: 1}, {Key: "createdAt", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var docs []Contact
	if err := cur.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func (s *Store) IncomingRequestsFor(ctx context.Context, toID bson.ObjectID) ([]Request, error) {
	cur, err := s.Requests.Find(ctx,
		bson.M{"toId": toID, "status": RequestPending},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}}),
	)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var docs []Request
	if err := cur.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func (s *Store) OutgoingRequestsFor(ctx context.Context, fromID bson.ObjectID) ([]Request, error) {
	cur, err := s.Requests.Find(ctx,
		bson.M{"fromId": fromID, "status": RequestPending},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}}),
	)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var docs []Request
	if err := cur.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}
