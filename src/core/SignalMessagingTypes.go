package core

type SignalingMessageType int

const (
	Ping                         SignalingMessageType = iota // 0
	Pong                                                     // 1
	WebRTC_HandshakeMessage                                  // 2
	WebRTC_ConnectionEstablished                             // 3
)
