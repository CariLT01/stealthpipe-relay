package bench

import "github.com/CariLT01/stealthpipe-relay-benchmarking/src/core"

func CreateRoom(benchmarker *core.Benchmarker) {
	roomCode := benchmarker.CreateRoom()

	if roomCode == "" {
		benchmarker.Logger.Error("Failed to create room")
		return
	}

	connected := benchmarker.ConnectSignal(roomCode)

	if !connected {
		benchmarker.Logger.Error("Failed to connect SIGNAL")
		return
	}

	for i := 0; i < core.CLIENTS_PER_ROOM; i++ {

		_, exists := benchmarker.GetRoomExists(roomCode)
		if !exists {
			benchmarker.Logger.Error("Room does not exist. Host crashed, skip creating rest of the clients")
			return
		}

		benchmarker.Logger.Info("creating client", "n", i, "roomCode", roomCode)
		benchmarker.ConnectClientToRelay(roomCode)
	}
}
