package data

import (
  "fmt"
  "io"
  "time"
  "encoding/json"

  "github.com/fitraditya/surelin-smtpd/config"
  "github.com/fitraditya/surelin-smtpd/log"
  "gopkg.in/mgo.v2/bson"
)

type DataStore struct {
  Config         config.DataStoreConfig
  Storage        interface{}
  SaveMailChan   chan *config.SMTPMessage
  NotifyMailChan chan interface{}
}

// DefaultDataStore creates a new DataStore object.
func NewDataStore() *DataStore {
  cfg := config.GetDataStoreConfig()

  // Database Writing
  saveMailChan := make(chan *config.SMTPMessage, 256)

  // Websocket Notification
  notifyMailChan := make(chan interface{}, 256)

  return &DataStore{Config: cfg, SaveMailChan: saveMailChan, NotifyMailChan: notifyMailChan}
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

    // start some savemail workers
    for i := 0; i < 3; i++ {
      go ds.SaveMail()
    }
  }
}

func (ds *DataStore) StorageDisconnect() {
  if ds.Config.Storage == "mongodb" {
    ds.Storage.(*MongoDB).Close()
  }
}

func (ds *DataStore) SaveMail() {
  log.LogTrace("Running SaveMail Routines")
  var err error
  var recon bool

  for {
    mc := <-ds.SaveMailChan
    msg := ParseSMTPMessage(mc, mc.Domain, ds.Config.MimeParser)

    if ds.Config.Storage == "mongodb" {
      mc.Hash, err = ds.Storage.(*MongoDB).Store(msg)

      // if mongo conection is broken, try to reconnect only once
      if err == io.EOF && !recon {
        log.LogWarn("Connection error trying to reconnect")
        ds.Storage = CreateMongoDB(ds.Config)
        recon = true

        // try to save again
        mc.Hash, err = ds.Storage.(*MongoDB).Store(msg)
      }

      if err == nil {
        recon = false
        log.LogTrace("Save Mail Client hash : <%s>", mc.Hash)
        mc.Notify <- 1

        // notify web socket
        ds.NotifyMailChan <- mc.Hash
      } else {
        mc.Notify <- -1
        log.LogError("Error storing message: %s", err)
      }
    }
  }
}

func (ds *DataStore) StatMails(username string) (nr int, size int) {
  messages, err := ds.Storage.(*MongoDB).Fetch(username)
  if err != nil {
    return 0, 0
  }

  var sum = 0
  // count how many letters there are in all the headers and messages
  for _, m := range *messages {
    for _, c := range m.Content.Headers {
      for _, h := range c {
        sum = sum + len(h)
      }
    }
    sum = sum + len(m.Content.Body)
  }

  // return the count and the size in octets (bytes)
  return len(*messages), sum*8
}

func (ds *DataStore) ListMails(username string) (nr int, size int, head []MessageHead) {
  messages, err := ds.Storage.(*MongoDB).Fetch(username)
  if err != nil {
    return 0, 0, nil
  }

  var sum = 0
  var heads []MessageHead
  // count how many letters there are in all the headers and messages
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
      Id: i+1,
      Uid: m.Id,
      Size: sz,
    }
    heads = append(heads, mh)
    sum = sum + size_ + len(m.Content.Body)
  }

  // return the count and the size in octets (bytes)
  return len(heads), sum*8, heads
}

func (ds *DataStore) GetMail(username string, id int) (message Message, size int) {
  messages, err := ds.Storage.(*MongoDB).FetchMailById(username, id)
  if err != nil {
    return Message{}, 0
  }

  // Get the specified message
	i := id-1

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

func (ds *DataStore) TopMail(username string, id int) (headers string) {
  messages, err := ds.Storage.(*MongoDB).Fetch(username)
  if err != nil {
    return ""
  }

  out, err := json.Marshal((*messages)[id-1].Content.Headers)

  // get the specified message
  return string(out)
}

func (ds *DataStore) CheckUserExists(username string) bool {
  user, err := ds.Storage.(*MongoDB).IsUserExists(username)
  if err != nil {
    return false
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

// Check if host address is in greylist
// h -> hostname client ip
func (ds *DataStore) CheckGreyHost(h string) bool {
  to, err := ds.Storage.(*MongoDB).IsGreyHost(h)
  if err != nil {
    return false
  }

  return to > 0
}

// Check if email address is in greylist
// t -> type (from/to)
// m -> local mailbox
// d -> domain
// h -> client IP
func (ds *DataStore) CheckGreyMail(t, m, d, h string) bool {
  e := fmt.Sprintf("%s@%s", m, d)
  to, err := ds.Storage.(*MongoDB).IsGreyMail(e, t)
  if err != nil {
    return false
  }

  return to > 0
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
