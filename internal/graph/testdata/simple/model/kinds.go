package model

import "io"

// ID is an alias; UserID is a new type that's neither struct nor interface.
type ID = int

type UserID int

func (u UserID) String() string { return "user" }

// ReadNamer embeds an interface; Tagged embeds a struct.
type ReadNamer interface {
	io.Reader
	Name() string
}

type Tagged struct {
	User
	Tag string
}
