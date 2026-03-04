package statistics

import (
	"sync/atomic"
	"time"

	"github.com/CariLT01/stealthPipeGoRelay/src/core"
)

func MonitorTraffic(app *core.ServerData) {
	ticker := time.NewTicker(1 * time.Second)
	for range ticker.C {
		rps := atomic.SwapUint64(&app.HandshakeCounter, 0)

		// 1. Determine what the difficulty SHOULD be based on RPS
		var targetDiff int32
		switch {
		case rps > uint64(app.Config.Difficulty7Threshold):
			targetDiff = 7 // Panic
		case rps > uint64(app.Config.Difficulty6Threshold):
			targetDiff = 6 // Busy
		default:
			targetDiff = 5 // Normal
		}

		// 2. Apply Cooldown Logic
		current := atomic.LoadInt32(&app.CurrentDifficulty)

		if targetDiff > current {
			// Moving UP: Do it immediately and reset the timer
			atomic.StoreInt32(&app.CurrentDifficulty, targetDiff)
			app.LastUpped = time.Now()
		} else if targetDiff < current {
			// Moving DOWN: Only allow if enough time has passed
			if time.Since(app.LastUpped) > time.Duration(app.Config.DifficultyCooldown) {
				atomic.StoreInt32(&app.CurrentDifficulty, targetDiff)
			}
		}
	}
}
