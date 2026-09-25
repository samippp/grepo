// Package store is part of the grepo test fixture; see ../main.go.
package store

import (
	"sync"

	"example.com/simple/model"
)

type Store struct {
	mu    sync.Mutex
	users map[int]model.User
}

func New() *Store { return &Store{users: map[int]model.User{}} }

func (s *Store) Get(id int) model.User {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.users[id]
}
