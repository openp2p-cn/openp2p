package openp2p

import (
	"errors"
)

// error message
var (
	// ErrorS2S string = "s2s is not supported"
	// ErrorHandshake string = "handshake error"
	ErrorS2S                 = errors.New("s2s is not supported")
	ErrorHandshake           = errors.New("handshake error")
	ErrorNewUser             = errors.New("new user")
	ErrorLogin               = errors.New("user or password not correct")
	ErrNodeTooShort          = errors.New("node name too short, it must >=8 charaters")
	ErrPeerOffline           = errors.New("peer offline")
	ErrNetwork               = errors.New("network error")
	ErrMsgFormat             = errors.New("message format wrong")
	ErrVersionNotCompatible  = errors.New("version not compatible")
	ErrOverlayConnDisconnect = errors.New("overlay connection is disconnected")
	ErrConnectRelayNode      = errors.New("connect relay node error")
)
