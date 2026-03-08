package core

import (
	"io"
	"time"

	"github.com/gorilla/websocket"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/time/rate"
)

func HandleSignalPacketFromClient(app *ServerData, message []byte, conn *websocket.Conn, gameId string) {
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
	case byte(WebRTC_HandshakeMessage):
		room.HostMu.Lock()
		room.Host.WriteMessage(websocket.BinaryMessage, message) // forward handshake data directly to the host
		room.HostMu.Unlock()
	case byte(WebRTC_ConnectionEstablished):
		// second byte is conn id
		app.Logger.Info("Client reported WebRTC Connection Established") // temporary
		// todo: replace with good logic such that it saves the state and logs on connection closed
	}

}

func (app *ServerData) ClientSignalToRelayHandler(conn *websocket.Conn, gameId string) {

	app.Logger.Info("New SIGNAL ws from Client")

	app.RoomsMu.RLock()
	room, exists := app.Rooms[gameId]
	app.RoomsMu.RUnlock()

	if !exists || room == nil {
		conn.Close()
		return
	}

	if !room.HasHost {
		app.Logger.Error("closed orphan client signal")
		conn.Close()
		return
	}

	defer app.handleCleanup(conn, gameId)
	defer app.NumberOfClientsConnected.Add(-1)

	buf := app.packetPool.Get().([]byte)
	defer app.packetPool.Put(buf[:cap(buf)])

	signalLimiter := rate.NewLimiter(rate.Limit(2), 10)

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

		HandleSignalPacketFromClient(app, buf[:n], conn, gameId)
		app.Statistics.packetsPerSecond.Add(app.Ctx, 1, metric.WithAttributes(
			attribute.String("flow", "signal-client"),
		))
		app.Statistics.bandwidth.Add(app.Ctx, int64(n), metric.WithAttributes(
			attribute.String("flow", "signal-client"),
		))

		conn.SetReadDeadline(time.Now().Add(time.Duration(app.Config.ReadDeadlineSecondsSignaling) * time.Second))
	}

}
