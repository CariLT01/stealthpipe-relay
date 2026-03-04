package core

/*
This file handles instance health, and kills the process or reports bad health when an unhealthy condition is detected.
*/

import (
	"os"
	"runtime"
	"time"
)

func healthCheckCorountines(app *ServerData) bool {
	count := runtime.NumGoroutine()
	if count > 5000 { // Adjust based on your expected max players
		app.Logger.Warn("UNHEALTHY: Goroutine leak detected",
			"count", count)
		return false
	}
	return true
}

func DeadlockWatchdog(app *ServerData) {
	ticker := time.NewTicker(5 * time.Second)
	for range ticker.C {
		// Create a channel to wait for the lock
		lockAcquired := make(chan bool, 1)

		go func() {
			app.RoomsMu.RLock()
			// Do nothing
			_ = len(app.Rooms) // do something useless to please the linter
			app.RoomsMu.RUnlock()
			lockAcquired <- true
		}()

		select {
		case <-lockAcquired:
			app.IsHealthy.Store(true)
		case <-time.After(10 * time.Second): // If we can't RLock in 10s, we are deadlocked

			app.IsHealthy.Store(false)

			app.Logger.Error("Deadlock detected on roomsMu. Dumping all goroutine stacks")

			// Create a buffer large enough for many goroutines
			buf := make([]byte, 1024*1024)
			n := runtime.Stack(buf, true) // 'true' gets stacks for ALL goroutines
			app.Logger.Info("", "stack", buf[:n])

		}

		if !healthCheckCorountines(app) {
			app.Logger.Error("Unhealthy instance: goroutine leak detected")
		}

		if !app.IsHealthy.Load() {
			if app.Config.TerminateWhenUnhealthy {
				app.Logger.Info("Terminating instance to force a restart")
				os.Exit(1)
			}
		}
	}
}
