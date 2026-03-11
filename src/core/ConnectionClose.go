package core

import "github.com/gorilla/websocket"

func (app *ServerData) CloseWebsocket(conn *websocket.Conn, reason CloseReasonType) {
	err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, reason))
	if err != nil {
		app.Logger.Info("Failed to write close message", "err", err)
	} else {
		app.Logger.Info("Sent close frame", "reason", reason)
	}
	conn.Close()
}
