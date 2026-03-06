package main

import (
	"time"

	"github.com/CariLT01/stealthpipe-relay-benchmarking/src/bench"
	"github.com/CariLT01/stealthpipe-relay-benchmarking/src/core"
)

func main() {
	benchmarker := core.NewBenchmarker()

	for i := 0; i < core.NUMBER_OF_ROOMS; i++ {
		bench.CreateRoom(benchmarker)
	}

	time.Sleep(1000 * time.Second)
}
