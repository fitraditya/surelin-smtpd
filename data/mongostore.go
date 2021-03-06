package data

import (
	"fmt"
	"strings"

	"github.com/fitraditya/surelin-smtpd/config"
	"github.com/fitraditya/surelin-smtpd/log"

	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

type MongoDB struct {
	Config   config.DataStoreConfig
	Session  *mgo.Session
	Messages *mgo.Collection
	Users    *mgo.Collection
	Hosts    *mgo.Collection
	Emails   *mgo.Collection
	Spamdb   *mgo.Collection
}

var (
	mgoSession *mgo.Session
)

func getSession(c config.DataStoreConfig) *mgo.Session {
	if mgoSession == nil {
		var err error
		mgoSession, err = mgo.Dial(c.MongoUri)

		if err != nil {
			log.LogError("Session Error connecting to MongoDB: %s", err)
			return nil
		}
	}

	return mgoSession.Clone()
}

func CreateMongoDB(c config.DataStoreConfig) *MongoDB {
	log.LogTrace("Connecting to MongoDB: %s\n", c.MongoUri)
	session, err := mgo.Dial(c.MongoUri)

	if err != nil {
		log.LogError("Error connecting to MongoDB: %s", err)
		return nil
	}

	return &MongoDB{
		Config:   c,
		Session:  session,
		Messages: session.DB(c.MongoDb).C(c.MongoColl),
		Users:    session.DB(c.MongoDb).C("Users"),
		Spamdb:   session.DB(c.MongoDb).C("SpamDB"),
	}
}

func (mongo *MongoDB) Close() {
	mongo.Session.Close()
}

func (mongo *MongoDB) Store(m *Message) (string, error) {
	err := mongo.Messages.Insert(m)

	if err != nil {
		log.LogError("Error inserting message: %s", err)
		return "", err
	}

	return m.Id, nil
}

// Login validates and returns a user object if they exist in the database.
func (mongo *MongoDB) Login(email, password string) (*User, error) {
	u := &User{}
	err := mongo.Users.Find(bson.M{"email": email}).One(&u)

	if err != nil {
		log.LogError("Login error: %v", err)
		return nil, err
	}

	if ok := Validate_Password(u.Password, password); !ok {
		log.LogError("Invalid Password: %s", u.Email)
		return nil, fmt.Errorf("Invalid Password!")
	}

	return u, nil
}

func (mongo *MongoDB) IsUserExists(email string) (*User, error) {
	u := &User{}
	err := mongo.Users.Find(bson.M{"email": email}).One(&u)

	if err != nil {
		log.LogError("Error finding user: %v", err)
		return nil, err
	}

	return u, nil
}

func (mongo *MongoDB) List(start int, limit int) (*Messages, error) {
	messages := &Messages{}
	err := mongo.Messages.Find(bson.M{}).Sort("-_id").Skip(start).Limit(limit).Select(bson.M{
		"id":          1,
		"from":        1,
		"to":          1,
		"attachments": 1,
		"created":     1,
		"ip":          1,
		"subject":     1,
		"starred":     1,
		"unread":      1,
	}).All(messages)

	if err != nil {
		log.LogError("Error loading messages: %s", err)
		return nil, err
	}

	return messages, nil
}

func (mongo *MongoDB) Total() (int, error) {
	total, err := mongo.Messages.Find(bson.M{}).Count()

	if err != nil {
		log.LogError("Error loading message: %s", err)
		return -1, err
	}

	return total, nil
}

func (mongo *MongoDB) Load(id string) (*Message, error) {
	result := &Message{}
	err := mongo.Messages.Find(bson.M{"id": id}).One(&result)

	if err != nil {
		log.LogError("Error loading message: %s", err)
		return nil, err
	}

	return result, nil
}

func (mongo *MongoDB) LoadAttachment(id string) (*Message, error) {
	result := &Message{}
	err := mongo.Messages.Find(bson.M{"attachments.id": id}).Select(bson.M{
		"id":            1,
		"attachments.$": 1,
	}).One(&result)

	if err != nil {
		log.LogError("Error loading attachment: %s", err)
		return nil, err
	}

	return result, nil
}

func (mongo *MongoDB) Fetch(email string) (*Messages, error) {
	s := strings.Split(email, "@")
	messages := &Messages{}
	err := mongo.Messages.Find(bson.M{"to": bson.M{"$elemMatch": bson.M{"mailbox": s[0], "domain": s[1]}}}).All(messages)

	if err != nil {
		log.LogError("Error loading messages: %s", err)
		return nil, err
	}

	return messages, nil
}

func (mongo *MongoDB) DeleteOne(id string) error {
	_, err := mongo.Messages.RemoveAll(bson.M{"id": id})
	return err
}

func (mongo *MongoDB) DeleteAll() error {
	_, err := mongo.Messages.RemoveAll(bson.M{})
	return err
}

func (mongo *MongoDB) StoreSpamIp(s SpamIP) (string, error) {
	err := mongo.Spamdb.Insert(s)
	if err != nil {
		log.LogError("Error inserting greylist ip: %s", err)
		return "", err
	}
	return s.Id.Hex(), nil
}
