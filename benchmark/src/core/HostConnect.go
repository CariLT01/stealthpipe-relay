package core

import (
	"bytes"
	"io"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
)

func (benchmarker *Benchmarker) GetURLSchemeJoin(isHost bool, gameId string, requestId string, version string) url.URL {
	u := url.URL{
		Scheme: "ws",
		Host:   benchmarker.RelayURLButWithoutTheHTTP,
		Path:   "/join", // ONLY the path here
	}

	// Use url.Values to build the query string safely
	q := u.Query()
	q.Set("id", gameId)
	q.Set("version", version)

	if isHost {
		q.Set("host", "true")
	}
	if requestId != "" {
		q.Set("request", requestId)
	}

	u.RawQuery = q.Encode() // This creates the "gameId=xxx&isHost=true..." string
	return u
}

func (benchmarker *Benchmarker) HostReadLoop(conn *websocket.Conn, gameId string) {

	benchmarker.Logger.Info("Start host read loop")

	defer benchmarker.HandleRoomCleanup(gameId)
	defer benchmarker.Logger.Info("End host read loop")
	defer conn.Close()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			benchmarker.Logger.Error("read error", "error", err)
			break
		}

		if bytes.HasPrefix(message, []byte("REQUESTCONNECTION")) {
			benchmarker.Logger.Info("detected connection request", "request", message)

			benchmarker.CreateHostToRelayConnection(gameId, string(message))
		}
	}
}

func (benchmarker *Benchmarker) HostWriteLoop(conn *websocket.Conn, gameId string) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	defer conn.Close()

	for range ticker.C {
		err := conn.WriteMessage(websocket.BinaryMessage, []byte{0x01})
		if err != nil {
			benchmarker.Logger.Error("write error", "error", err)
			break
		}
	}

}

func (benchmarker *Benchmarker) ConnectSignal(gameId string) bool {

	room, exists := benchmarker.GetRoomExists(gameId)

	if !exists {
		benchmarker.Logger.Error("Room ID does not exist")
		return false
	}

	urlScheme := benchmarker.GetURLSchemeJoin(true, gameId, "", "4.0.0")
	conn, res, err := websocket.DefaultDialer.Dial(urlScheme.String(), nil)

	if err != nil {
		if res != nil {
			benchmarker.Logger.Error("Handshake status code", "status", res.Status)
			body, _ := io.ReadAll(res.Body)
			benchmarker.Logger.Error("Handshake error body", "body", string(body))
		}

		benchmarker.Logger.Error("Failed to dial relay", "error", err)
		return false
	}

	room.HostWriteMu.Lock()
	room.Host = conn
	room.HostWriteMu.Unlock()

	go benchmarker.HostReadLoop(conn, gameId)
	go benchmarker.HostWriteLoop(conn, gameId)

	return true

}
