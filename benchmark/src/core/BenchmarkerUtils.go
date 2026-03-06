package core

func (benchmarker *Benchmarker) GetRoomExists(gameId string) (*Room, bool) {
	benchmarker.RoomsMu.RLock()
	room, exists := benchmarker.Rooms[gameId]
	benchmarker.RoomsMu.RUnlock()

	return room, exists
}
