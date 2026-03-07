package core

import (
	"context"
	"encoding/base64"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/time/rate"

	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

type Room struct {
	Host                          *websocket.Conn
	HostMu                        *sync.Mutex
	HostIP                        string
	LastRoomFilledTime            atomic.Int64
	ClientsToHostConnections      map[*websocket.Conn]*websocket.Conn
	HostToClientsConnections      map[*websocket.Conn]*websocket.Conn
	RequestedConnectionsMap       map[string]*websocket.Conn
	RequestedConnectionsMapMutex  *sync.RWMutex
	ClientsToHostConnectionsMutex *sync.RWMutex
	HostToClientsConnectionsMutex *sync.RWMutex
	HostOutboundLimiter           *rate.Limiter
	ClientsLimiter                *rate.Limiter
	CreatedTime                   int64
	HasHost                       bool
}

type ServerConstructorExtraConfig struct {
	UseGrafana  bool
	GrafanaKey  string
	GrafanaUrl  string
	GrafanaUser string
}

type ServerStatistics struct {
	packetsPerSecond         metric.Int64Counter
	numberOfClientsConnected metric.Int64Gauge
	requestsPerSecond        metric.Int64Counter
	bandwidth                metric.Int64Counter
}

type ServerData struct {
	Rooms                    map[string]*Room
	RoomsMu                  sync.RWMutex
	Upgrader                 websocket.Upgrader
	LastTick                 int64
	PacketCounter            atomic.Int64
	PacketsPerSecond         atomic.Int64
	NumberOfClientsConnected atomic.Int64

	HandshakeCounter  uint64
	CurrentDifficulty int32
	LastUpped         time.Time
	UsedSalts         map[string]int64
	UsedSaltsMutex    sync.Mutex
	IsHealthy         atomic.Bool

	Logger *slog.Logger

	Config *ServerConfig

	packetPool *sync.Pool
	smallPool  *sync.Pool

	Slim bool

	Mux *http.ServeMux

	Statistics *ServerStatistics

	OtlpMetricExplorter *otlpmetrichttp.Exporter
	MeterProvider       *sdkmetric.MeterProvider
	RelayMeter          metric.Meter
	Ctx                 context.Context
}

type ServerConfig struct {
	// Versioning
	RelayVersion string

	// Bandwidth throttling (optimized for 6 players)
	PacketThrottlingOutboundHost  int // bytes/sec from host -> clients
	PacketThrottlingBurstOutbound int // burst bytes
	PacketThrottlingInboundHost   int // bytes/sec from clients -> host
	PacketThrottlingBurstInbound  int // burst bytes
	SignalingMaximumPacketSize    int // maximum packet size in bytes in signal connection

	// Packet size
	PacketMaximumSize int // maximum packet size in bytes

	// Proof-of-work difficulty
	Difficulty6Threshold int   // trigger difficulty 6
	Difficulty7Threshold int   // trigger difficulty 7
	DifficultyCooldown   int64 // cooldown in milliseconds

	// Cleanup
	RoomEmptyCleanupDelay int64 // milliseconds
	RoomIdleNoClientDelay int64 // milliseconds

	// Stability and instance health
	TerminateWhenUnhealthy       bool
	ReadDeadlineSecondsSignaling int

	// Tokens
	reuseTokenExpiryHours int
	powTokenExpiryMinutes int

	// Grafana
	UsingGrafana bool
	GrafanaUrl   string
	GrafanaKey   string
	GrafanaUser  string
}

func NewServerConfig(conf ServerConstructorExtraConfig) *ServerConfig {

	return &ServerConfig{
		RelayVersion:                  "4.1.0",    // relay protocol version
		PacketThrottlingOutboundHost:  1_500_000,  // 1.5 MB
		PacketThrottlingBurstOutbound: 12_000_000, // 12 MB
		PacketThrottlingInboundHost:   300_000,    // 300 KB
		PacketThrottlingBurstInbound:  4_000_000,  // 4 MB
		PacketMaximumSize:             2_200_000,  // 2.2 MB
		SignalingMaximumPacketSize:    50_000,     // 50 kb (generous)

		Difficulty6Threshold: 20,     // 20 rps
		Difficulty7Threshold: 100,    // 100 rps
		DifficultyCooldown:   30_000, // milliseconds

		RoomEmptyCleanupDelay:        60_000,      // milliseconds
		RoomIdleNoClientDelay:        60_000 * 15, // milliseconds
		TerminateWhenUnhealthy:       true,
		ReadDeadlineSecondsSignaling: 30,

		reuseTokenExpiryHours: 3,
		powTokenExpiryMinutes: 5,

		UsingGrafana: conf.UseGrafana,
		GrafanaUrl:   conf.GrafanaUrl,
		GrafanaKey:   conf.GrafanaKey,
		GrafanaUser:  conf.GrafanaUser,
	}
}

func NewServerStatistics(meter metric.Meter) *ServerStatistics {

	var realMeter metric.Meter

	if meter == nil {
		realMeter = noop.NewMeterProvider().Meter("noop")
	} else {
		realMeter = meter
	}

	packetsPerSecondMeter, err := realMeter.Int64Counter("packets_per_second")
	numberOfClientsMeter, err := realMeter.Int64Gauge("clients_connected")
	bandwidthMeter, err := realMeter.Int64Counter("bandwidth")
	requestsPerSecondMeter, err := realMeter.Int64Counter("requests_per_second")

	if err != nil {

	}

	return &ServerStatistics{
		packetsPerSecond:         packetsPerSecondMeter,
		numberOfClientsConnected: numberOfClientsMeter,
		bandwidth:                bandwidthMeter,
		requestsPerSecond:        requestsPerSecondMeter,
	}
}

var opts = &slog.HandlerOptions{
	AddSource: true, // include file:line
}

var logger = slog.New(slog.NewJSONHandler(os.Stdout, opts))

func NewEmptyExtraConfig() ServerConstructorExtraConfig {

	return ServerConstructorExtraConfig{
		UseGrafana:  false,
		GrafanaKey:  "",
		GrafanaUrl:  "",
		GrafanaUser: "",
	}
}

func NewServer(slim bool, ext ServerConstructorExtraConfig) *ServerData {

	config := NewServerConfig(ext)

	var exporter *otlpmetrichttp.Exporter
	var err error
	var provider *sdkmetric.MeterProvider
	var relayMeter metric.Meter
	var res *resource.Resource

	ctx := context.Background()

	if ext.UseGrafana {

		// check for is prod

		serviceName := "relay-service-staging"
		if os.Getenv("PRODUCTION") != "" {
			serviceName = "relay-service-prod"
		}

		res, err = resource.Merge(
			resource.Default(),
			resource.NewWithAttributes(
				semconv.SchemaURL,
				semconv.ServiceNameKey.String(serviceName), // This is what Grafana looks for
				semconv.ServiceVersionKey.String(config.RelayVersion),
			),
		)

		auth := base64.StdEncoding.EncodeToString([]byte(ext.GrafanaUser + ":" + ext.GrafanaKey))
		exporter, err = otlpmetrichttp.New(context.Background(), otlpmetrichttp.WithEndpoint("otlp-gateway-prod-ca-east-0.grafana.net"),
			otlpmetrichttp.WithURLPath("/otlp/v1/metrics"),
			otlpmetrichttp.WithHeaders(
				map[string]string{
					"Authorization": "Basic " + auth,
				},
			))
		if err != nil {
			logger.Error("Failed to create OLTP Metric HTTP Exporter. Not exporting statistics for this session")
			exporter = nil
		}

		provider = sdkmetric.NewMeterProvider(
			sdkmetric.WithResource(res),
			sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter,
				sdkmetric.WithInterval(15*time.Second))),
		)

		if err := runtime.Start(runtime.WithMeterProvider(provider)); err != nil {
			logger.Error("Failed to start runtime metrics", "error", err)
		}

		relayMeter = provider.Meter("relay-service")

		// 1. Create a channel to catch the "Quit" signal from Render
		gracefulStop := make(chan os.Signal, 1)
		signal.Notify(gracefulStop, syscall.SIGTERM, syscall.SIGINT)

		go func() {
			// 2. Wait for the signal
			sig := <-gracefulStop
			logger.Info("Caught signal Shutting down provider...", "signal", sig)

			// 3. Force the flush
			// We use a timeout so the app doesn't hang forever if the network is dead
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if err := provider.Shutdown(shutdownCtx); err != nil {
				logger.Error("OTLP shutdown failed", "error", err)
			}

			// 4. Actually exit the program
			os.Exit(0)
		}()

	}

	return &ServerData{
		Rooms:                    make(map[string]*Room),
		RoomsMu:                  sync.RWMutex{},
		Upgrader:                 websocket.Upgrader{ReadBufferSize: 16384, WriteBufferSize: 16384},
		LastTick:                 time.Now().UnixMilli(),
		PacketCounter:            atomic.Int64{},
		PacketsPerSecond:         atomic.Int64{},
		NumberOfClientsConnected: atomic.Int64{},
		CurrentDifficulty:        5,
		UsedSalts:                make(map[string]int64),
		UsedSaltsMutex:           sync.Mutex{},
		IsHealthy:                atomic.Bool{},
		Logger:                   logger,
		Config:                   config,
		packetPool: &sync.Pool{
			New: func() any {
				return make([]byte, config.PacketMaximumSize)
			},
		},
		smallPool: &sync.Pool{
			New: func() any {
				return make([]byte, 64*1024)
			},
		},

		Slim: slim,
		Mux:  http.NewServeMux(),

		OtlpMetricExplorter: exporter,
		MeterProvider:       provider,
		RelayMeter:          relayMeter,
		Ctx:                 ctx,
		Statistics:          NewServerStatistics(relayMeter),
	}
}
