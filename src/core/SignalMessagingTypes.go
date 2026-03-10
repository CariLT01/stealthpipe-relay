package core

type SignalingMessageType int

const (
	Idle                         SignalingMessageType = iota // 0
	Ping                                                     // 1
	Pong                                                     // 2
	WebRTC_HandshakeMessage                                  // 3
	WebRTC_ConnectionEstablished                             // 4
	WebRTC_RequestConnection                                 // 5
	WebRTC_ConnectionFailed                                  // 6
	WebRTC_ConnectionReady                                   // 7
)
