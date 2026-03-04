package core

func (app *ServerData) GetRoomExists(roomId string) (*Room, bool) {
	app.RoomsMu.RLock()
	room, exists := app.Rooms[roomId]
	app.RoomsMu.RUnlock()

	return room, exists
}
