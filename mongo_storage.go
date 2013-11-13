package main

import (
	"errors"
	"labix.org/v2/mgo"
)

type MongoStorage struct {
	mongoUrl    string
	mongoDbName string
	sess        *mgo.Session
	db          *mgo.Database
}

func NewMongoStorage(mongoUrl, mongoDbName string) *MongoStorage {
	return &MongoStorage{mongoUrl, mongoDbName, nil, nil}
}

func (m *MongoStorage) Applications() (Iterator, error) {
	if m.db == nil {
		return nil, errors.New("Not connected to mongo")
	}

	col := m.db.C("applications")
	return col.Find(nil).Iter(), nil
}

func (m *MongoStorage) Routes() (Iterator, error) {
	if m.db == nil {
		return nil, errors.New("Not connected to mongo")
	}
	col := m.db.C("routes")
	return col.Find(nil).Iter(), nil
}

func (m *MongoStorage) Open() error {
	var err error
	m.sess, err = mgo.Dial(m.mongoUrl)
	if err != nil {
		return err
	}

	m.sess.SetMode(mgo.Monotonic, true)
	m.db = m.sess.DB(m.mongoDbName)

	return nil
}

func (m *MongoStorage) Close() {
	m.db = nil
	m.sess.Close()
	m.sess = nil
}
