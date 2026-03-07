package main

import (
	"net/http"
	"time"

	"github.com/CariLT01/stealthpipe-relay-benchmarking/src/bench"
	"github.com/CariLT01/stealthpipe-relay-benchmarking/src/core"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func StatsTicker(benchmarker *core.Benchmarker) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	go func() {
		http.Handle("/metrics", promhttp.Handler())
		http.ListenAndServe(":2112", nil)
	}()

	for range ticker.C {

		benchmarker.PPSRecv.Store(benchmarker.PacketsCounterRecv.Swap(0))
		benchmarker.PPSSent.Store(benchmarker.PacketsCounterSent.Swap(0))

		benchmarker.Logger.Info("Packets per second SENT", "pps", benchmarker.PPSSent.Load())
		benchmarker.Logger.Info("Packets per second RECV", "pps", benchmarker.PPSRecv.Load())

		core.OpsSent.Set(float64(benchmarker.PPSSent.Load()))
		core.OpsRecv.Set(float64(benchmarker.PPSRecv.Load()))

		ratioReceived := float32(1.0)

		core.RatioRecv.Set(float64(ratioReceived))

		if benchmarker.PPSSent.Load() != 0 {
			ratioReceived = float32(benchmarker.PPSRecv.Load()) / float32(benchmarker.PPSSent.Load())
		}

		benchmarker.Logger.Info("RATIO recv", "ratio", ratioReceived)
	}
}

func main() {
	benchmarker := core.NewBenchmarker()

	go StatsTicker(benchmarker)

	for i := 0; i < core.NUMBER_OF_ROOMS; i++ {
		bench.CreateRoom(benchmarker)
	}

	time.Sleep(1000 * time.Second)
}
