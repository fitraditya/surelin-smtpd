package data

import (
	"encoding/json"
	"io"
	"time"

	"github.com/fitraditya/surelin-smtpd/config"
	"github.com/fitraditya/surelin-smtpd/log"
	"gopkg.in/mgo.v2/bson"
)

type DataStore struct {
	Config       config.DataStoreConfig
	Storage      interface{}
	SaveMailChan chan *config.SMTPMessage
}

// DefaultDataStore creates a new DataStore object.
func NewDataStore() *DataStore {
	cfg := config.GetDataStoreConfig()

	// Database Writing
	saveMailChan := make(chan *config.SMTPMessage, 256)

	return &DataStore{Config: cfg, SaveMailChan: saveMailChan}
}

func (ds *DataStore) StorageConnect() {
	if ds.Config.Storage == "mongodb" {
		log.LogInfo("Trying MongoDB storage")
		s := CreateMongoDB(ds.Config)

		if s == nil {
			log.LogInfo("MongoDB storage unavailable")
		} else {
			log.LogInfo("Using MongoDB storage")
			ds.Storage = s
		}

		// Start some savemail workers
		for i := 0; i < 3; i++ {
			go ds.SaveMail(i)
		}
	}
}

func (ds *DataStore) StorageDisconnect() {
	if ds.Config.Storage == "mongodb" {
		ds.Storage.(*MongoDB).Close()
	}
}

func (ds *DataStore) CheckUserExists(email string) bool {
	user, err := ds.Storage.(*MongoDB).IsUserExists(email)

	if err != nil {
		return true
	}

	if user != nil {
		return true
	}

	return false
}

func (ds *DataStore) LoginUser(email string, password string) bool {
	user, err := ds.Storage.(*MongoDB).Login(email, password)

	if err != nil {
		return false
	}

	if user != nil {
		return true
	}

	return false
}

func (ds *DataStore) SaveMail(id int) {
	log.LogTrace("Running Save Mail Daemon #<%d>", id)
	var err error
	var recon bool

	for {
		mc := <-ds.SaveMailChan
		msg := ParseSMTPMessage(mc, mc.Domain, true)

		if ds.Config.Storage == "mongodb" {
			mc.Hash, err = ds.Storage.(*MongoDB).Store(msg)

			// If mongo conection is broken, try to reconnect only once
			if err == io.EOF && !recon {
				log.LogWarn("Connection error trying to reconnect")
				ds.Storage = CreateMongoDB(ds.Config)
				recon = true

				// Try to save again
				mc.Hash, err = ds.Storage.(*MongoDB).Store(msg)
			}

			if err == nil {
				recon = false
				log.LogTrace("Save Mail Client hash : <%s>", mc.Hash)
				mc.Notify <- 1
			} else {
				mc.Notify <- -1
				log.LogError("Error storing message: %s", err)
			}
		}
	}
}

func (ds *DataStore) StatMails(email string) (nr int, size int) {
	messages, err := ds.Storage.(*MongoDB).Fetch(email)

	if err != nil {
		return 0, 0
	}

	var sum = 0

	// Count how many letters there are in all the headers and messages
	for _, m := range *messages {
		for _, c := range m.Content.Headers {
			for _, h := range c {
				sum = sum + len(h)
			}
		}

		sum = sum + len(m.Content.Body)
	}

	// Return the count and the size in octets (bytes)
	return len(*messages), sum * 8
}

func (ds *DataStore) ListMails(email string) (nr int, size int, head []MessageHead) {
	messages, err := ds.Storage.(*MongoDB).Fetch(email)

	if err != nil {
		return 0, 0, nil
	}

	var sum = 0
	var heads []MessageHead

	// Count how many letters there are in all the headers and messages
	for i, m := range *messages {
		sz := 0

		for _, c := range m.Content.Headers {
			for _, h := range c {
				sz = sz + len(h)
			}
		}

		size_ := sz
		sz = sz + len(m.Content.Body)*8
		mh := MessageHead{
			Id:   i + 1,
			Uid:  m.Id,
			Size: sz,
		}
		heads = append(heads, mh)
		sum = sum + size_ + len(m.Content.Body)
	}

	// Return the count and the size in octets (bytes)
	return len(heads), sum * 8, heads
}

func (ds *DataStore) GetMail(email string, id int) (message Message, size int) {
	messages, err := ds.Storage.(*MongoDB).Fetch(email)

	if err != nil {
		return Message{}, 0
	}

	// Get the specified message
	i := id - 1
	m := (*messages)[i]
	sz := 0

	for _, c := range m.Content.Headers {
		for _, h := range c {
			sz = sz + len(h)
		}
	}

	sz = sz + len(m.Content.Body)*8
	return m, sz
}

func (ds *DataStore) DeleteMail(email string, id int) bool {
	messages, err := ds.Storage.(*MongoDB).Fetch(email)

	if err != nil {
		return false
	}

	// Get the specified message
	i := id - 1
	m := (*messages)[i]
	sz := 0

	for _, c := range m.Content.Headers {
		for _, h := range c {
			sz = sz + len(h)
		}
	}

	sz = sz + len(m.Content.Body)*8
	return true
}

func (ds *DataStore) TopMail(email string, id int) (headers string) {
	messages, err := ds.Storage.(*MongoDB).Fetch(email)

	if err != nil {
		return ""
	}

	out, err := json.Marshal((*messages)[id-1].Content.Headers)

	// Get the specified message
	return string(out)
}

func (ds *DataStore) SaveSpamIP(ip string, email string) {
	s := SpamIP{
		Id:        bson.NewObjectId(),
		CreatedAt: time.Now(),
		IsActive:  true,
		Email:     email,
		IPAddress: ip,
	}

	if _, err := ds.Storage.(*MongoDB).StoreSpamIp(s); err != nil {
		log.LogError("Error inserting Spam IPAddress: %s", err)
	}
}
