// Package store is the data layer: Mongo connection, collection handles and
// the document shapes. Business rules do not live here.
package store

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/tktaofik/capacity-takehome/api/internal/capacity"
)

type User struct {
	ID   bson.ObjectID `bson:"_id,omitempty"`
	Name string        `bson:"name"`
}

// Contact is one side of a pair. Adding a contact writes two of these, one for
// each user; removing it must free both.
type Contact struct {
	ID        bson.ObjectID `bson:"_id,omitempty"`
	OwnerID   bson.ObjectID `bson:"ownerId"`
	OtherID   bson.ObjectID `bson:"otherId"`
	Tier      capacity.Tier `bson:"tier"`
	CreatedAt time.Time     `bson:"createdAt"`
}

type RequestStatus string

const (
	RequestPending  RequestStatus = "PENDING"
	RequestAccepted RequestStatus = "ACCEPTED"
	RequestDeclined RequestStatus = "DECLINED"
)

type Request struct {
	ID        bson.ObjectID `bson:"_id,omitempty"`
	FromID    bson.ObjectID `bson:"fromId"`
	ToID      bson.ObjectID `bson:"toId"`
	Tier      capacity.Tier `bson:"tier"`
	Status    RequestStatus `bson:"status"`
	CreatedAt time.Time     `bson:"createdAt"`
}

type Store struct {
	Client    *mongo.Client
	DB        *mongo.Database
	Users     *mongo.Collection
	Contacts  *mongo.Collection
	Requests  *mongo.Collection
	UserSeats *mongo.Collection
}

func Connect(ctx context.Context, uri string) (*Store, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}
	if err := client.Ping(ctx, nil); err != nil {
		return nil, err
	}
	db := client.Database("capacity")
	s := &Store{
		Client:    client,
		DB:        db,
		Users:     db.Collection("users"),
		Contacts:  db.Collection("contacts"),
		Requests:  db.Collection("requests"),
		UserSeats: db.Collection("userSeats"),
	}
	return s, s.ensureIndexes(ctx)
}

// ensureIndexes declares the one index the app cannot be correct without: a
// pair may exist only once per owner. Any index you need for the capacity
// rules is yours to add here.
func (s *Store) ensureIndexes(ctx context.Context) error {
	_, err := s.Contacts.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "ownerId", Value: 1}, {Key: "otherId", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("owner_other_unique"),
	})
	return err
}

// Seed inserts a few people to talk to, so the client has something to show on
// first run. It is idempotent.
func (s *Store) Seed(ctx context.Context) error {
	names := []string{"You", "Ada", "Grace", "Alan", "Katherine", "Barbara", "Edsger", "Radia", "Ken", "Margaret"}
	for _, n := range names {
		filter := bson.M{"name": n}
		update := bson.M{"$setOnInsert": bson.M{"name": n}}
		if _, err := s.Users.UpdateOne(ctx, filter, update, options.UpdateOne().SetUpsert(true)); err != nil {
			return err
		}
	}
	return nil
}

// CountsFor returns the caller's active contacts per tier.
func (s *Store) CountsFor(ctx context.Context, ownerID bson.ObjectID) (capacity.Counts, error) {
	counts := capacity.Counts{}
	cur, err := s.Contacts.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"ownerId": ownerID}}},
		{{Key: "$group", Value: bson.M{"_id": "$tier", "n": bson.M{"$sum": 1}}}},
	})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var rows []struct {
		Tier capacity.Tier `bson:"_id"`
		N    int           `bson:"n"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, err
	}
	for _, r := range rows {
		counts[r.Tier] = r.N
	}
	return counts, nil
}
