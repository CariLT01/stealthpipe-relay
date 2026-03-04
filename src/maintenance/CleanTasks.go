package maintenance

import (
	"time"

	"github.com/CariLT01/stealthPipeGoRelay/src/core"
	"github.com/gorilla/websocket"
)

func CleanUnusedRooms(app *core.ServerData) {
	// 15 seconds is a good frequency for a "Pre-Host" check
	ticker := time.NewTicker(15 * time.Second)

	for range ticker.C {
		var staleRooms []string
		now := time.Now().UnixMilli()

		// Phase 1: Quick Scan with Read Lock
		app.RoomsMu.RLock()
		for code, room := range app.Rooms {
			// Logic: No host joined AND the room creation was too long ago
			if !room.HasHost && (now-room.CreatedTime) > app.Config.RoomEmptyCleanupDelay {
				staleRooms = append(staleRooms, code)
			}
		}
		app.RoomsMu.RUnlock()

		// Phase 2: Targeted Deletion with Write Lock
		if len(staleRooms) > 0 {
			app.RoomsMu.Lock()
			for _, code := range staleRooms {
				// Double-check existence and status (it might have changed during lock switch)
				if room, exists := app.Rooms[code]; exists && !room.HasHost {
					delete(app.Rooms, code)
					app.Logger.Debug("Pruned ghost room", "code", code)
				}
			}
			app.RoomsMu.Unlock()
		}
	}
}

func CleanIdleRooms(app *core.ServerData) {
	ticker := time.NewTicker(35 * time.Second)
	for range ticker.C {
		var toClose []*websocket.Conn
		var codesToDelete []string
		now := time.Now().UnixMilli()

		app.RoomsMu.RLock()
		for code, room := range app.Rooms {
			if now-room.LastRoomFilledTime.Load() > app.Config.RoomIdleNoClientDelay && len(room.ClientsToHostConnections) == 0 {
				codesToDelete = append(codesToDelete, code)
				if room.Host != nil {
					toClose = append(toClose, room.Host)
				}
			}
		}
		app.RoomsMu.RUnlock()

		if len(codesToDelete) > 0 {
			app.RoomsMu.Lock()
			for _, code := range codesToDelete {
				delete(app.Rooms, code)
			}
			app.RoomsMu.Unlock()

			// Close connections AFTER unlocking roomsMu
			for _, conn := range toClose {
				conn.Close()
				app.Logger.Debug("Closed idle connection")
			}
		}
	}
}
