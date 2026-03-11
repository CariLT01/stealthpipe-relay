package core

import (
	"io"
	"time"

	"github.com/gorilla/websocket"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/time/rate"
)

func HandleSignalPacket(app *ServerData, message []byte, conn *websocket.Conn, gameId string) {
	/*

		bytes:
		0 - keep-alive
		1 - ping
		2 - pong
	*/

	app.RoomsMu.RLock()
	room, exists := app.Rooms[gameId]
	app.RoomsMu.RUnlock()

	if !exists {
		return
	}

	if len(message) == 0 {
		return
	}
	packetType := message[0]

	switch packetType {
	// only act for ping, and return pong
	case byte(Ping):
		room.HostMu.Lock() // lock to prevent race condition
		conn.WriteMessage(websocket.BinaryMessage, []byte{byte(Pong)})
		room.HostMu.Unlock()
	case byte(Pong):
		// do noting
	default:
		// just forward whatever else we need
		// second byte is conn id

		if len(message) < 2 {
			app.Logger.Error("Cannot send to client, no client ID provided.", "packetType", packetType)
			return
		}

		clientId := message[1]
		otherSignConn, exists := room.WebRTCHandshakeConnectionsMap[clientId]

		if !exists {
			app.Logger.Error("Failed to forward WebRTC Signaling Message! Connection ID not found in map", "connectionId", clientId)
			return
		}

		// forward to the other
		app.Logger.Info("forwarding bytes", "length", len(message))
		otherSignConn.WriteMessage(websocket.BinaryMessage, message)
	}

}

func (app *ServerData) HostSignalToRelayHandler(conn *websocket.Conn, gameId string) {

	app.Logger.Info("New SIGNAL ws from Server")

	app.RoomsMu.RLock()
	room, exists := app.Rooms[gameId]
	app.RoomsMu.RUnlock()

	if !exists || room == nil {
		return
	}

	defer app.handleCleanup(conn, gameId)
	defer app.NumberOfClientsConnected.Add(-1)

	buf := app.packetPool.Get().([]byte)
	defer app.packetPool.Put(buf[:cap(buf)])

	signalLimiter := rate.NewLimiter(rate.Limit(app.Config.SignalMaxPPS), app.Config.SignalBurstPPS)

	conn.SetReadDeadline(time.Now().Add(time.Duration(app.Config.ReadDeadlineSecondsSignaling) * time.Second))

	for {

		messageType, reader, err := conn.NextReader()
		if err != nil {
			app.Logger.Error("Got error", "error", err.Error())
			break
		}

		if messageType != websocket.BinaryMessage {
			app.Logger.Warn("Warning: Message not binary, skipping")
			continue
		}

		lr := io.LimitReader(reader, int64(app.Config.PacketMaximumSize)+1)
		n, err := io.ReadAtLeast(lr, buf, 1)
		if err != nil && err != io.EOF {
			break
		}

		if n > int(app.Config.SignalingMaximumPacketSize) {
			app.Logger.Info("Packet too large, kicking client.")
			break // This exits the loop and triggers handleCleanup
		}
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			app.Logger.Error("Got error: ", "error", err.Error())
			break
		}

		if !signalLimiter.Allow() {
			app.Logger.Warn("Abuse detected on signal relay, ending connection")
			break
		}

		HandleSignalPacket(app, buf[:n], conn, gameId)
		app.Statistics.packetsPerSecond.Add(app.Ctx, 1, metric.WithAttributes(
			attribute.String("flow", "signal"),
		))
		app.Statistics.bandwidth.Add(app.Ctx, int64(n), metric.WithAttributes(
			attribute.String("flow", "signal"),
		))

		conn.SetReadDeadline(time.Now().Add(time.Duration(app.Config.ReadDeadlineSecondsSignaling) * time.Second))
	}

}
