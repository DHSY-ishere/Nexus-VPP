// Package integrations switches on the optional infrastructure from environment variables and
// mounts its HTTP routes. Anything not configured stays off and the system runs without it.
//
//	REDIS_ADDR         host:port[,host:port...]  live fleet state (several addresses = Redis Cluster)
//	REDIS_PASSWORD
//	TIMESCALE_DSN      postgres://user:pass@host:port/db   telemetry history (TimescaleDB)
//	MQTT_BROKER        tcp://host:1883             edge telemetry in, dispatch commands out
//	FORECAST_GRPC      host:50051                  the Python forecast service
//	OPENADR            1 to run an OpenADR 3.0-style VTN and a VEN that drives demand response
//	OPENADR_TOKEN      bearer token required by the VTN (recommended)
//	OPENADR_VTN_URL    VTN the VEN polls (default: this server)
//
// Without REDIS_ADDR the fleet state is kept in memory, so /fleet/summary always works.
package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DHSY-ishere/Nexus-VPP/backend/engine"
	"github.com/DHSY-ishere/Nexus-VPP/backend/fleetstate"
	"github.com/DHSY-ishere/Nexus-VPP/backend/forecastclient"
	"github.com/DHSY-ishere/Nexus-VPP/backend/ingestion"
	"github.com/DHSY-ishere/Nexus-VPP/backend/models"
	"github.com/DHSY-ishere/Nexus-VPP/backend/mqttbridge"
	"github.com/DHSY-ishere/Nexus-VPP/backend/openadr"
	"github.com/DHSY-ishere/Nexus-VPP/backend/telemetrystore"
)

// Config is read from the environment.
type Config struct {
	RedisAddrs    []string
	RedisPassword string
	TimescaleDSN  string
	MQTTBroker    string
	ForecastGRPC  string
	OpenADR       bool
	OpenADRToken  string
	OpenADRURL    string
	Port          string
}

// FromEnv reads the configuration.
func FromEnv() Config {
	c := Config{
		RedisPassword: os.Getenv("REDIS_PASSWORD"), TimescaleDSN: os.Getenv("TIMESCALE_DSN"),
		MQTTBroker: os.Getenv("MQTT_BROKER"), ForecastGRPC: os.Getenv("FORECAST_GRPC"),
		OpenADR: os.Getenv("OPENADR") == "1", OpenADRToken: os.Getenv("OPENADR_TOKEN"),
		OpenADRURL: os.Getenv("OPENADR_VTN_URL"), Port: os.Getenv("PORT"),
	}
	for _, a := range strings.Split(os.Getenv("REDIS_ADDR"), ",") {
		if a = strings.TrimSpace(a); a != "" {
			c.RedisAddrs = append(c.RedisAddrs, a)
		}
	}
	if c.Port == "" {
		c.Port = "8080"
	}
	if c.OpenADRURL == "" {
		c.OpenADRURL = "http://127.0.0.1:" + c.Port
	}
	return c
}

// Services are the running integrations.
type Services struct {
	cfg      Config
	Fleet    fleetstate.Store
	redis    *fleetstate.Redis
	TS       *telemetrystore.Store
	tsSink   *telemetrystore.Sink
	MQTT     *mqttbridge.Bridge
	Forecast *forecastclient.Client
	VTN      *openadr.VTN
	VEN      *openadr.VEN
	status   map[string]string
	sigMu    sync.RWMutex
	signal   openadr.Signal
}

func (s *Services) note(name, state string) {
	s.status[name] = state
	log.Printf("integration %-10s %s", name, state)
}

// Start brings up whatever is configured. It returns the extra ingestion sinks (fleet state
// and telemetry history) to add next to Kafka. A service that fails to start is reported
// and left off; it does not stop the others or the backend.
func Start(ctx context.Context, cfg Config, eng *engine.Engine, mux *http.ServeMux) (*Services, []ingestion.Sink) {
	s := &Services{cfg: cfg, status: map[string]string{}}
	var sinks []ingestion.Sink

	// Fleet state: Redis if configured and reachable, otherwise memory.
	s.Fleet = fleetstate.NewMemory()
	s.note("fleetstate", "memory")
	if len(cfg.RedisAddrs) > 0 {
		r := fleetstate.NewRedis(cfg.RedisAddrs, cfg.RedisPassword)
		pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		if err := r.Ping(pctx); err != nil {
			s.note("fleetstate", fmt.Sprintf("memory (redis %v unreachable: %v)", cfg.RedisAddrs, err))
			r.Close()
		} else {
			s.Fleet, s.redis = r, r
			s.note("fleetstate", fmt.Sprintf("redis %v", cfg.RedisAddrs))
		}
		cancel()
	}
	sinks = append(sinks, fleetstate.Sink{Store: s.Fleet})
	go func() { // forget nodes that stopped reporting
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.Fleet.Sweep(ctx, 2*time.Minute)
			}
		}
	}()

	if cfg.TimescaleDSN != "" {
		store, err := telemetrystore.Open(ctx, cfg.TimescaleDSN)
		if err != nil {
			s.note("timescale", "off ("+err.Error()+")")
		} else {
			s.TS = store
			s.tsSink = telemetrystore.NewSink(store.Insert, 500, 500*time.Millisecond)
			sinks = append(sinks, s.tsSink)
			if store.Hypertable {
				s.note("timescale", "on (hypertable)")
			} else {
				s.note("timescale", "on (plain PostgreSQL table, timescaledb extension not active)")
			}
		}
	}

	if cfg.MQTTBroker != "" {
		b, err := mqttbridge.Start(cfg.MQTTBroker, "nexus-bridge", ingestion.TelemetryStream)
		if err != nil {
			s.note("mqtt", "off ("+err.Error()+")")
		} else {
			s.MQTT = b
			s.note("mqtt", "on "+cfg.MQTTBroker)
		}
	}

	if cfg.ForecastGRPC != "" {
		c, err := forecastclient.Dial(cfg.ForecastGRPC)
		if err != nil {
			s.note("forecast", "off ("+err.Error()+")")
		} else {
			s.Forecast = c
			s.note("forecast", "grpc "+cfg.ForecastGRPC)
		}
	}

	if cfg.OpenADR {
		s.VTN = openadr.NewVTN(cfg.OpenADRToken)
		s.VTN.Register(mux)
		s.VEN = openadr.NewVEN(cfg.OpenADRURL, cfg.OpenADRToken, zoneGroups(eng))
		go s.VEN.Run(ctx, 2*time.Second, func(sig openadr.Signal) {
			s.sigMu.Lock()
			s.signal = sig
			s.sigMu.Unlock()
			eng.SetDR(sig.Level, sig.Price)
			log.Printf("openadr: demand-response level %d, price %.3f (event %q)", sig.Level, sig.Price, sig.EventID)
		}, func(err error) { log.Printf("openadr: poll failed, keeping last signal: %v", err) })
		note := "on, VTN at " + openadr.BasePath
		if cfg.OpenADRToken == "" {
			note += " (NO TOKEN: any client may publish events)"
		}
		s.note("openadr", note)
	}

	s.routes(mux)
	return s, sinks
}

func zoneGroups(eng *engine.Engine) []string {
	g := []string{"ALL"}
	for i := 1; i <= 16; i++ {
		g = append(g, fmt.Sprintf("Z%d", i))
	}
	return g
}

// Close stops the services and flushes what is buffered.
func (s *Services) Close() {
	if s.tsSink != nil {
		s.tsSink.Close()
	}
	if s.TS != nil {
		s.TS.Close()
	}
	if s.MQTT != nil {
		s.MQTT.Close()
	}
	if s.Forecast != nil {
		s.Forecast.Close()
	}
	if s.redis != nil {
		s.redis.Close()
	}
}

func json200(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func errJSON(w http.ResponseWriter, code int, msg string) {
	json200(w, code, map[string]string{"error": msg})
}

func (s *Services) routes(mux *http.ServeMux) {
	mux.HandleFunc("/integrations/status", func(w http.ResponseWriter, r *http.Request) {
		out := map[string]any{"services": s.status}
		if s.MQTT != nil {
			out["mqtt"] = map[string]int64{"accepted": s.MQTT.Accepted.Load(), "rejected": s.MQTT.Rejected.Load(), "dropped": s.MQTT.Dropped.Load()}
		}
		if s.VTN != nil {
			s.sigMu.RLock()
			out["openadr_signal"] = s.signal
			s.sigMu.RUnlock()
		}
		if s.Forecast != nil {
			ok, ver, err := s.Forecast.Ready(r.Context())
			out["forecast"] = map[string]any{"ready": ok, "model": ver, "error": fmt.Sprint(err)}
		}
		json200(w, http.StatusOK, out)
	})

	mux.HandleFunc("/fleet/summary", func(w http.ResponseWriter, r *http.Request) {
		sum, err := s.Fleet.Summary(r.Context())
		if err != nil {
			errJSON(w, http.StatusInternalServerError, err.Error())
			return
		}
		json200(w, http.StatusOK, sum)
	})

	mux.HandleFunc("/telemetry/recent", func(w http.ResponseWriter, r *http.Request) {
		if s.TS == nil {
			errJSON(w, http.StatusServiceUnavailable, "TimescaleDB is not configured (set TIMESCALE_DSN)")
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 || limit > 1000 {
			limit = 100
		}
		rows, err := s.TS.Recent(r.Context(), r.URL.Query().Get("node"), limit)
		if err != nil {
			errJSON(w, http.StatusInternalServerError, err.Error())
			return
		}
		if rows == nil {
			rows = []models.TelemetryEvent{}
		}
		json200(w, http.StatusOK, rows)
	})

	mux.HandleFunc("/telemetry/buckets", func(w http.ResponseWriter, r *http.Request) {
		if s.TS == nil {
			errJSON(w, http.StatusServiceUnavailable, "TimescaleDB is not configured (set TIMESCALE_DSN)")
			return
		}
		minutes, _ := strconv.Atoi(r.URL.Query().Get("minutes"))
		if minutes <= 0 || minutes > 7*24*60 {
			minutes = 60
		}
		width, _ := strconv.Atoi(r.URL.Query().Get("bucket_s"))
		if width < 1 {
			width = 60
		}
		b, err := s.TS.Buckets(r.Context(), time.Duration(width)*time.Second, time.Now().Add(-time.Duration(minutes)*time.Minute))
		if err != nil {
			errJSON(w, http.StatusInternalServerError, err.Error())
			return
		}
		if b == nil {
			b = []telemetrystore.Bucket{}
		}
		json200(w, http.StatusOK, b)
	})

	// /forecast?hour=14&history=0.97,0.9,0.7  (clear-sky index readings, oldest first)
	mux.HandleFunc("/forecast", func(w http.ResponseWriter, r *http.Request) {
		if s.Forecast == nil {
			errJSON(w, http.StatusServiceUnavailable, "forecast service is not configured (set FORECAST_GRPC)")
			return
		}
		hour, err := strconv.ParseFloat(r.URL.Query().Get("hour"), 64)
		if err != nil {
			errJSON(w, http.StatusBadRequest, "hour is required, e.g. hour=14.5")
			return
		}
		var hist []float64
		for _, p := range strings.Split(r.URL.Query().Get("history"), ",") {
			if p = strings.TrimSpace(p); p == "" {
				continue
			}
			v, err := strconv.ParseFloat(p, 64)
			if err != nil {
				errJSON(w, http.StatusBadRequest, "history must be comma-separated numbers")
				return
			}
			hist = append(hist, v)
		}
		f, err := s.Forecast.Forecast(r.Context(), r.URL.Query().Get("node"), hour, hist)
		if err != nil {
			errJSON(w, http.StatusBadGateway, err.Error())
			return
		}
		json200(w, http.StatusOK, f)
	})
}
