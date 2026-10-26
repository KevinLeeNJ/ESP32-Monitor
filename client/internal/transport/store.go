package transport

import (
	"encoding/json"
	"errors"
	"esp32-monitor/client/internal/transport/wire"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Pairing struct {
	DeviceID       string `json:"device_id"`
	DeviceName     string `json:"device_name"`
	PairingID      string `json:"pairing_id"`
	DeviceToken    string `json:"device_token"`
	SerialKey      string `json:"serial_key"`
	ConsentAt      int64  `json:"consent_at"`
	WarningVersion int    `json:"warning_version"`
	LANConsent     bool   `json:"lan_consent"`
}
type storeData struct {
	HostID string             `json:"host_id"`
	Pairs  map[string]Pairing `json:"pairs"`
}

// Store is a dedicated credential vault. No public status DTO embeds Pairing.
// Windows uses a protected DACL; Unix requires a 0700 directory and 0600 file.
type Store struct {
	mu   sync.Mutex
	root *os.Root
	data storeData
}

func OpenStore(dir string) (*Store, error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	if e := secureStoreDirectory(dir); e != nil {
		return nil, e
	}
	i, e := os.Lstat(dir)
	if e != nil || !i.IsDir() || i.Mode()&os.ModeSymlink != 0 || !privateStorePath(dir, i) {
		return nil, errors.New("credential_directory_not_private")
	}
	root, e := os.OpenRoot(dir)
	if e != nil {
		return nil, e
	}
	s := &Store{root: root, data: storeData{wire.NewID(), map[string]Pairing{}}}
	i, e = root.Lstat("companion.json")
	if os.IsNotExist(e) {
		e = s.save()
	} else if e == nil {
		if !i.Mode().IsRegular() || !privateStorePath(filepath.Join(dir, "companion.json"), i) || i.Size() > 16384 {
			e = errors.New("invalid_credential_store")
		} else {
			var b []byte
			b, e = root.ReadFile("companion.json")
			if e == nil {
				e = json.Unmarshal(b, &s.data)
			}
		}
	}
	if e == nil {
		if !wire.UUID(s.data.HostID) || len(s.data.Pairs) > 8 {
			e = errors.New("invalid_credential_store")
		}
		for id, p := range s.data.Pairs {
			_, a := wire.RawNonce(p.DeviceToken)
			_, b := wire.RawNonce(p.SerialKey)
			if id != p.DeviceID || !wire.UUID(id) || !wire.UUID(p.PairingID) || a != nil || b != nil || p.ConsentAt <= 0 || p.WarningVersion != 1 {
				e = errors.New("invalid_credential_store")
			}
		}
	}
	if e != nil {
		root.Close()
		return nil, e
	}
	return s, nil
}
func (s *Store) save() error {
	b, e := json.Marshal(s.data)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(s.root.Name(), ".companion-*")
	if e != nil {
		return e
	}
	name := filepath.Base(f.Name())
	defer s.root.Remove(name)
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	return s.root.Rename(name, "companion.json")
}
func (s *Store) HostID() string { s.mu.Lock(); defer s.mu.Unlock(); return s.data.HostID }
func (s *Store) pair(id string) (Pairing, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.data.Pairs[id]
	return p, ok
}
func (s *Store) pairs() []Pairing {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := make([]Pairing, 0, len(s.data.Pairs))
	for _, v := range s.data.Pairs {
		p = append(p, v)
	}
	return p
}
func (s *Store) put(p Pairing) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data.Pairs[p.DeviceID]; ok {
		return errors.New("session_conflict")
	}
	if len(s.data.Pairs) >= 8 {
		return errors.New("capacity_exceeded")
	}
	s.data.Pairs[p.DeviceID] = p
	if e := s.save(); e != nil {
		delete(s.data.Pairs, p.DeviceID)
		return e
	}
	return nil
}
func (s *Store) remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.data.Pairs[id]
	delete(s.data.Pairs, id)
	if e := s.save(); e != nil {
		if ok {
			s.data.Pairs[id] = p
		}
		return e
	}
	return nil
}
func (s *Store) Close() error { return s.root.Close() }

type DeviceStatus struct {
	IdentityVerified bool      `json:"identity_verified"`
	DeviceID         string    `json:"device_id"`
	Name             string    `json:"device_name"`
	Paired           bool      `json:"paired"`
	Address          string    `json:"address"`
	ConfirmedOnline  bool      `json:"confirmed_online"`
	ConnectionState  string    `json:"connection_state"`
	SessionID        string    `json:"session_id"`
	LastSeen         time.Time `json:"last_seen"`
}

func (s *Store) consent(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.data.Pairs[id]
	if !ok {
		return errors.New("unauthorized")
	}
	old := p
	p.LANConsent = true
	p.ConsentAt = time.Now().Unix()
	p.WarningVersion = 1
	s.data.Pairs[id] = p
	if e := s.save(); e != nil {
		s.data.Pairs[id] = old
		return e
	}
	return nil
}
