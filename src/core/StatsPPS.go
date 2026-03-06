package core

import "time"

func (app *ServerData) StatsPPSTicker() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		app.PacketsPerSecond.Store(app.PacketCounter.Swap(0))
	}
}
