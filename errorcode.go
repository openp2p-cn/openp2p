package main

import (
	"errors"
)

// error message
var (
	// ErrorS2S string = "s2s is not supported"
	// ErrorHandshake string = "handshake error"
	ErrorS2S       = errors.New("s2s is not supported")
	ErrorHandshake = errors.New("handshake error")
)
