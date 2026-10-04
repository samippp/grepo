package store

// Package-level calls run at startup; their caller is the package (#7).
var defaultStore = New()

var Timeout int

var a, b = 1, 2

// One call for two names.
var first, err = load()

func load() (int, error) { return 0, nil }

// Blank vars get no node, but register still runs at startup.
var _ Getter = (*Store)(nil)
var _ = register()

func register() bool { return true }

const maxUsers = 100

const greeting string = "hi"

const (
	Red = iota
	Green
	Blue
)
