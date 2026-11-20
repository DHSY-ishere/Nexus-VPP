// Package mqttbridge connects edge nodes to the backend over MQTT.
//
// Edge agents publish telemetry as JSON to  nexus/telemetry/<node_id>  and listen for
// dispatch commands on  nexus/commands/<node_id> . MQTT suits the edge: small headers,
// persistent sessions and QoS 1 delivery over flaky home networks. The bridge subscribes
// to every node's telemetry, validates each message and pushes it into the ingestion
// buffer, and publishes commands back down to a node.
//
// Telemetry from the wire is untrusted, so each message is checked before it is accepted:
// well-formed JSON, a node id that matches the topic it was published on (a node cannot
// speak for another), and physically plausible values.
package mqttbridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync/atomic"
	"time"

	"github.com/DHSY-ishere/Nexus-VPP/backend/models"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

const (
	TelemetryPrefix = "nexus/telemetry/"
	CommandPrefix   = "nexus/commands/"
	maxPayload      = 4096
)

// ParseTelemetry validates one MQTT telemetry message.
func ParseTelemetry(topic string, payload []byte) (models.TelemetryEvent, error) {
	var ev models.TelemetryEvent
	if !strings.HasPrefix(topic, TelemetryPrefix) {
		return ev, fmt.Errorf("unexpected topic %q", topic)
	}
	if len(payload) > maxPayload {
		return ev, errors.New("payload too large")
	}
	if err := json.Unmarshal(payload, &ev); err != nil {
		return ev, fmt.Errorf("bad json: %w", err)
	}
	node := strings.TrimPrefix(topic, TelemetryPrefix)
	if node == "" || strings.ContainsAny(node, "/+#") {
		return ev, fmt.Errorf("bad node id in topic %q", topic)
	}
	if ev.NodeID != node {
		return ev, fmt.Errorf("node_id %q does not match topic node %q", ev.NodeID, node)
	}
	for name, v := range map[string]float64{
		"consumption": ev.CurrentConsumption, "replenishment": ev.ReplenishmentRate, "soc": ev.StateOfCharge,
		"frequency": ev.LocalFrequency, "voltage": ev.Voltage,
	} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return ev, fmt.Errorf("%s is not a finite number", name)
		}
	}
	switch {
	case ev.StateOfCharge < 0 || ev.StateOfCharge > 100:
		return ev, fmt.Errorf("state of charge %.1f outside 0-100", ev.StateOfCharge)
	case ev.LocalFrequency < 40 || ev.LocalFrequency > 60:
		return ev, fmt.Errorf("frequency %.2f Hz outside 40-60", ev.LocalFrequency)
	case ev.CurrentConsumption < 0 || ev.ReplenishmentRate < 0:
		return ev, errors.New("negative power")
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	return ev, nil
}

// Bridge is a running MQTT-to-ingestion bridge.
type Bridge struct {
	client   mqtt.Client
	out      chan<- models.TelemetryEvent
	Accepted atomic.Int64
	Rejected atomic.Int64 // failed validation
	Dropped  atomic.Int64 // valid, but the ingestion buffer was full
}

// Start connects to the broker (for example tcp://127.0.0.1:1883) and begins forwarding
// telemetry into out.
func Start(brokerURL, clientID string, out chan<- models.TelemetryEvent) (*Bridge, error) {
	b := &Bridge{out: out}
	opts := mqtt.NewClientOptions().AddBroker(brokerURL).SetClientID(clientID).
		SetAutoReconnect(true).SetConnectRetry(true).SetConnectRetryInterval(2 * time.Second).
		SetCleanSession(false).SetKeepAlive(30 * time.Second)
	// Resubscribe on every (re)connection.
	opts.SetOnConnectHandler(func(c mqtt.Client) {
		c.Subscribe(TelemetryPrefix+"#", 1, b.onMessage)
	})
	b.client = mqtt.NewClient(opts)
	tok := b.client.Connect()
	if !tok.WaitTimeout(10 * time.Second) {
		return nil, errors.New("mqtt: connect timed out")
	}
	if err := tok.Error(); err != nil {
		return nil, err
	}
	return b, nil
}

func (b *Bridge) onMessage(_ mqtt.Client, m mqtt.Message) {
	ev, err := ParseTelemetry(m.Topic(), m.Payload())
	if err != nil {
		b.Rejected.Add(1)
		return
	}
	select {
	case b.out <- ev:
		b.Accepted.Add(1)
	default: // never block the MQTT client's network goroutine
		b.Dropped.Add(1)
	}
}

// PublishCommand sends a dispatch command to one node (QoS 1).
func (b *Bridge) PublishCommand(nodeID string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tok := b.client.Publish(CommandPrefix+nodeID, 1, false, raw)
	if !tok.WaitTimeout(5 * time.Second) {
		return errors.New("mqtt: publish timed out")
	}
	return tok.Error()
}

// Close disconnects from the broker.
func (b *Bridge) Close() { b.client.Disconnect(250) }
