package store

import "example.com/simple/model"

// Cache implements Getter with a value receiver, so Cache and *Cache both do.
// *Store implements it with a pointer receiver, so only *Store does.
type Cache struct{}

func (c Cache) Get(id int) model.User { return model.User{ID: id} }
