package store

// Reset is declared through the alias S but belongs to Store.
type S = Store

func (s *S) Reset() {}
