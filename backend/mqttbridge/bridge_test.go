package mqttbridge

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DHSY-ishere/Nexus-VPP/backend/models"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	mochi "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
)

func good(node string) models.TelemetryEvent {
	return models.TelemetryEvent{NodeID: node, NodeType: "PROSUMER", Timestamp: time.Now().UTC(),
		CurrentConsumption: 1.2, ReplenishmentRate: 3.4, StateOfCharge: 66, LocalFrequency: 49.9, Voltage: 231}
}

func TestParseTelemetry(t *testing.T) {
	enc := func(v any) []byte { b, _ := json.Marshal(v); return b }
	bad := func(mut func(*models.TelemetryEvent)) []byte { e := good("n1"); mut(&e); return enc(e) }
	cases := []struct {
		name, topic string
		payload     []byte
		ok          bool
	}{
		{"valid", "nexus/telemetry/n1", enc(good("n1")), true},
		{"wrong topic prefix", "other/n1", enc(good("n1")), false},
		{"node cannot impersonate another", "nexus/telemetry/n2", enc(good("n1")), false},
		{"wildcard in topic", "nexus/telemetry/n+", enc(good("n+")), false},
		{"empty node", "nexus/telemetry/", enc(good("")), false},
		{"not json", "nexus/telemetry/n1", []byte("{nope"), false},
		{"soc above 100", "nexus/telemetry/n1", bad(func(e *models.TelemetryEvent) { e.StateOfCharge = 140 }), false},
		{"soc negative", "nexus/telemetry/n1", bad(func(e *models.TelemetryEvent) { e.StateOfCharge = -1 }), false},
		{"frequency absurd", "nexus/telemetry/n1", bad(func(e *models.TelemetryEvent) { e.LocalFrequency = 5 }), false},
		{"negative power", "nexus/telemetry/n1", bad(func(e *models.TelemetryEvent) { e.CurrentConsumption = -3 }), false},
		{"oversized", "nexus/telemetry/n1", []byte(strings.Repeat("x", maxPayload+1)), false},
	}
	for _, c := range cases {
		_, err := ParseTelemetry(c.topic, c.payload)
		if (err == nil) != c.ok {
			t.Errorf("%s: err = %v, want ok=%v", c.name, err, c.ok)
		}
	}
	ev, _ := ParseTelemetry("nexus/telemetry/n1", enc(models.TelemetryEvent{NodeID: "n1", StateOfCharge: 5, LocalFrequency: 50}))
	if ev.Timestamp.IsZero() {
		t.Error("missing timestamp should be stamped")
	}
}

// brokerURL returns a real broker if MQTT_URL is set (docker compose up mosquitto), otherwise
// starts an embedded one so the test needs no external service.
func brokerURL(t *testing.T) string {
	if u := os.Getenv("MQTT_URL"); u != "" {
		return u
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	srv := mochi.New(nil)
	if err := srv.AddHook(new(auth.AllowHook), nil); err != nil {
		t.Fatal(err)
	}
	if err := srv.AddListener(listeners.NewTCP(listeners.Config{ID: "t", Address: addr})); err != nil {
		t.Fatal(err)
	}
	go srv.Serve()
	t.Cleanup(func() { srv.Close() })
	time.Sleep(200 * time.Millisecond)
	return "tcp://" + addr
}

func client(t *testing.T, url, id string) mqtt.Client {
	c := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(url).SetClientID(id))
	if tok := c.Connect(); !tok.WaitTimeout(5*time.Second) || tok.Error() != nil {
		t.Fatalf("connect %s: %v", id, tok.Error())
	}
	t.Cleanup(func() { c.Disconnect(100) })
	return c
}

func TestEndToEndOverARealBroker(t *testing.T) {
	url := brokerURL(t)
	out := make(chan models.TelemetryEvent, 100)
	b, err := Start(url, fmt.Sprintf("bridge-%d", time.Now().UnixNano()), out)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	time.Sleep(300 * time.Millisecond) // subscription settles

	edge := client(t, url, fmt.Sprintf("edge-%d", time.Now().UnixNano()))
	pub := func(topic string, v any) {
		raw, _ := json.Marshal(v)
		edge.Publish(topic, 1, false, raw).WaitTimeout(3 * time.Second)
	}
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("home-%d", i)
		pub(TelemetryPrefix+id, good(id))
	}
	pub(TelemetryPrefix+"mallory", good("victim")) // impersonation
	pub(TelemetryPrefix+"broken", map[string]any{"node_id": "broken", "state_of_charge_percent": 900, "local_frequency_hz": 50})

	got := map[string]bool{}
	deadline := time.After(5 * time.Second)
	for len(got) < 20 {
		select {
		case ev := <-out:
			got[ev.NodeID] = true
		case <-deadline:
			t.Fatalf("received only %d of 20 events (accepted %d rejected %d)", len(got), b.Accepted.Load(), b.Rejected.Load())
		}
	}
	time.Sleep(300 * time.Millisecond)
	if b.Accepted.Load() != 20 || b.Rejected.Load() != 2 {
		t.Fatalf("accepted %d (want 20), rejected %d (want 2)", b.Accepted.Load(), b.Rejected.Load())
	}
}

func TestCommandsReachTheNode(t *testing.T) {
	url := brokerURL(t)
	b, err := Start(url, fmt.Sprintf("bridge-cmd-%d", time.Now().UnixNano()), make(chan models.TelemetryEvent, 1))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	node := client(t, url, fmt.Sprintf("node-%d", time.Now().UnixNano()))
	got := make(chan map[string]any, 1)
	node.Subscribe(CommandPrefix+"home-7", 1, func(_ mqtt.Client, m mqtt.Message) {
		var v map[string]any
		json.Unmarshal(m.Payload(), &v)
		got <- v
	}).WaitTimeout(3 * time.Second)

	if err := b.PublishCommand("home-7", map[string]any{"type": "EXECUTE_TRADE", "energy_kw": 2.5}); err != nil {
		t.Fatal(err)
	}
	select {
	case v := <-got:
		if v["type"] != "EXECUTE_TRADE" || v["energy_kw"] != 2.5 {
			t.Fatalf("unexpected command %v", v)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("command never arrived")
	}
}

func TestFullBufferDropsInsteadOfBlocking(t *testing.T) {
	url := brokerURL(t)
	out := make(chan models.TelemetryEvent) // unbuffered and never read: always full
	b, err := Start(url, fmt.Sprintf("bridge-full-%d", time.Now().UnixNano()), out)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	time.Sleep(300 * time.Millisecond)
	edge := client(t, url, fmt.Sprintf("edge-full-%d", time.Now().UnixNano()))
	raw, _ := json.Marshal(good("n1"))
	for i := 0; i < 5; i++ {
		edge.Publish(TelemetryPrefix+"n1", 1, false, raw).WaitTimeout(3 * time.Second)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && b.Dropped.Load() < 5 {
		time.Sleep(50 * time.Millisecond)
	}
	if b.Dropped.Load() != 5 {
		t.Fatalf("dropped %d, want 5 (the MQTT client must never block)", b.Dropped.Load())
	}
}
