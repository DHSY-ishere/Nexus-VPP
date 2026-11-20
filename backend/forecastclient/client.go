// Package forecastclient calls the Python forecast service over gRPC
// (contract: proto/forecast.proto, server: ml/grpc_server.py).
package forecastclient

import (
	"context"
	"fmt"
	"time"

	pb "github.com/DHSY-ishere/Nexus-VPP/backend/forecastpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Forecast is the 15-minute-ahead solar forecast for one node.
type Forecast struct {
	ClearSkyIndex15m   float64 `json:"clear_sky_index_15m"`   // predicted index in 15 minutes
	SolarFraction15m   float64 `json:"solar_fraction_15m"`    // predicted output as a fraction of installed kWp
	PersistenceIndex15 float64 `json:"persistence_index_15m"` // naive forecast: index unchanged
	ModelVersion       string  `json:"model_version"`
}

// Client wraps a gRPC connection to the forecast service.
type Client struct {
	conn    *grpc.ClientConn
	stub    pb.ForecasterClient
	Timeout time.Duration
}

// Dial connects to the service (lazily: the first call establishes the connection).
func Dial(target string, opts ...grpc.DialOption) (*Client, error) {
	if len(opts) == 0 {
		opts = []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	}
	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		return nil, fmt.Errorf("forecast service %s: %w", target, err)
	}
	return &Client{conn: conn, stub: pb.NewForecasterClient(conn), Timeout: 2 * time.Second}, nil
}

// Close releases the connection.
func (c *Client) Close() error { return c.conn.Close() }

// Forecast asks for the 15-minute-ahead forecast from a node's recent clear-sky-index history
// (oldest first, 5-minute steps).
func (c *Client) Forecast(ctx context.Context, nodeID string, hourOfDay float64, history []float64) (Forecast, error) {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	r, err := c.stub.Forecast(ctx, &pb.ForecastRequest{NodeId: nodeID, HourOfDay: hourOfDay, ClearSkyIndexHistory: history})
	if err != nil {
		return Forecast{}, err
	}
	return Forecast{r.ClearSkyIndex_15M, r.SolarFraction_15M, r.PersistenceIndex_15M, r.ModelVersion}, nil
}

// Ready reports whether the service is up and its model loaded.
func (c *Client) Ready(ctx context.Context) (bool, string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	r, err := c.stub.Health(ctx, &pb.HealthRequest{})
	if err != nil {
		return false, "", err
	}
	return r.Ready, r.ModelVersion, nil
}
