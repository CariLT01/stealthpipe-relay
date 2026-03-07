package core

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Registry for our metrics
	OpsSent = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "benchmarker_pps_sent",
		Help: "Packets sent per second",
	})
	OpsRecv = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "benchmarker_pps_recv",
		Help: "Packets received per second",
	})
	RatioRecv = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "benchmarker_ratio_received",
		Help: "Ratio of received vs sent packets",
	})
)
