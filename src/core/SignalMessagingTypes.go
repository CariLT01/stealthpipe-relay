package core

type SignalingMessageType int

const (
	Ping                         SignalingMessageType = iota // 0
	Pong                                                     // 1
	WebRTC_HandshakeMessage                                  // 2
	WebRTC_ConnectionEstablished                             // 3
	WebRTC_RequestConnection                                 // 4
	WebRTC_ConnectionFailed                                  // 5
	WebRTC_ConnectionReady
)
