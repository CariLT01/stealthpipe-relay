package core

func (benchmarker *Benchmarker) HandleRoomCleanup(gameId string) {
	// when host is down, disconnect all clients, remove room

	room, exists := benchmarker.GetRoomExists(gameId)

	if !exists {
		benchmarker.Logger.Error("Failed to cleanup, room does not exist")
	}

	for _, client := range room.Clients {
		client.Close()
	}

	for _, client := range room.HostConnections {
		client.Close()
	}

	// just in case, close host
	room.HostWriteMu.Lock()
	room.Host.Close()
	room.HostWriteMu.Unlock()

	// delete room
	benchmarker.RoomsMu.Lock()
	delete(benchmarker.Rooms, gameId)
	benchmarker.RoomsMu.Unlock()

	benchmarker.Logger.Info("Cleaned up room code", "gameId", gameId)
}
