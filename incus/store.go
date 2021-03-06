package incus

import "sync"

type Storage struct {
	memory      *MemoryStore
	StorageType string

	userMu sync.RWMutex
	pageMu sync.RWMutex
}

func InitStore(Config *Configuration) *Storage {
	store_type := "memory"

	var Store = Storage{
		&MemoryStore{make(map[string]map[string]*Socket), make(map[string]map[string]*Socket), 0},
		store_type,

		sync.RWMutex{},
		sync.RWMutex{},
	}

	return &Store
}

func (this *Storage) Save(sock *Socket) error {
	this.userMu.Lock()
	this.memory.Save(sock)
	this.userMu.Unlock()

	return nil
}

func (this *Storage) Remove(sock *Socket) error {
	this.userMu.Lock()
	this.memory.Remove(sock)
	this.userMu.Unlock()

	return nil
}

func (this *Storage) Client(UID string) (map[string]*Socket, error) {
	defer this.userMu.RUnlock()
	this.userMu.RLock()

	return this.memory.Client(UID)
}

func (this *Storage) Clients() map[string]map[string]*Socket {
	defer this.userMu.RUnlock()
	this.userMu.RLock()

	return this.memory.Clients()
}

func (this *Storage) ClientList() ([]string, error) {
	return nil, nil
}

func (this *Storage) Count() (int64, error) {
	return this.memory.Count()
}

func (this *Storage) SetPage(sock *Socket) error {
	this.pageMu.Lock()
	this.memory.SetPage(sock)
	this.pageMu.Unlock()

	return nil
}

func (this *Storage) UnsetPage(sock *Socket) error {
	this.pageMu.Lock()
	this.memory.UnsetPage(sock)
	this.pageMu.Unlock()

	return nil
}

func (this *Storage) getPage(page string) map[string]*Socket {
	defer this.pageMu.RUnlock()
	this.pageMu.RLock()
	return this.memory.getPage(page)
}
