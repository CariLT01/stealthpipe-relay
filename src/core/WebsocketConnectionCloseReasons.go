package core

type CloseReasonType = string

var WebsocketConnectionCloseReason = struct {
	HostDisconnected        CloseReasonType
	ConnectionHighBandwidth CloseReasonType
	ConnectionHighUsage     CloseReasonType
	SocketReadFailed        CloseReasonType
	SocketWriteFailed       CloseReasonType
	ConnectionIdle          CloseReasonType
	RoomNotFound            CloseReasonType
	RoomNoHost              CloseReasonType
	RoomBadState            CloseReasonType
	InternalError           CloseReasonType
	InvalidRequestID        CloseReasonType
	PairLinkFailed          CloseReasonType
	InvalidAction           CloseReasonType
	ReuseTokenPresented     CloseReasonType
	Unknown                 CloseReasonType
	ClientDisconnected      CloseReasonType
	SignalDisconnected      CloseReasonType
	Unspecified             CloseReasonType
	PacketTooLarge          CloseReasonType
	BadRequest              CloseReasonType
	InvalidVersion          CloseReasonType
	OutdatedClient          CloseReasonType
	UnsupportedClient       CloseReasonType
}{
	HostDisconnected:        "HOST_DISCONNECTED",
	ConnectionHighBandwidth: "HIGH_BANDWIDTH",
	ConnectionHighUsage:     "HIGH_USAGE",
	SocketReadFailed:        "WSS_READ_FAILED",
	SocketWriteFailed:       "WSS_WRITE_FAILED",
	ConnectionIdle:          "WSS_IDLE",
	RoomNotFound:            "ROOM_NOT_FOUND",
	RoomNoHost:              "ROOM_NO_HOST",
	RoomBadState:            "ROOM_BAD_STATE",
	InternalError:           "INTERNAL_ERROR",
	InvalidRequestID:        "INVALID_REQUEST_ID",
	PairLinkFailed:          "PAIR_LINK_FAILED",
	InvalidAction:           "INVALID_ACTION",
	ReuseTokenPresented:     "REUSE_TOKEN_PRESENTED",
	Unknown:                 "UNKNOWN",
	ClientDisconnected:      "CLIENT_DISCONNECTED",
	Unspecified:             "UNSPECIFIED",
	PacketTooLarge:          "PACKET_TOO_LARGE",
	SignalDisconnected:      "SIGNAL_DISCONNECTED",
	BadRequest:              "BAD_REQUEST",
	InvalidVersion:          "INVALID_VERSION",
	OutdatedClient:          "OUTDATED_CLIENT",
	UnsupportedClient:       "UNSUPPORTED_CLIENT",
}
