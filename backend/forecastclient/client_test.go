package forecastclient

import (
	"context"
	"net"
	"testing"

	pb "github.com/DHSY-ishere/Nexus-VPP/backend/forecastpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type fake struct {
	pb.UnimplementedForecasterServer
}

func (fake) Forecast(_ context.Context, r *pb.ForecastRequest) (*pb.ForecastResponse, error) {
	if len(r.ClearSkyIndexHistory) == 0 {
		return nil, status.Error(codes.InvalidArgument, "empty history")
	}
	newest := r.ClearSkyIndexHistory[len(r.ClearSkyIndexHistory)-1]
	return &pb.ForecastResponse{ClearSkyIndex_15M: newest / 2, SolarFraction_15M: newest / 4,
		PersistenceIndex_15M: newest, ModelVersion: "fake"}, nil
}
func (fake) Health(context.Context, *pb.HealthRequest) (*pb.HealthResponse, error) {
	return &pb.HealthResponse{Ready: true, ModelVersion: "fake"}, nil
}

func newClient(t *testing.T) *Client {
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	pb.RegisterForecasterServer(srv, fake{})
	go srv.Serve(lis)
	t.Cleanup(srv.Stop)
	c, err := Dial("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestForecastRoundTrip(t *testing.T) {
	c := newClient(t)
	f, err := c.Forecast(context.Background(), "n1", 13, []float64{0.9, 0.8, 0.6})
	if err != nil {
		t.Fatal(err)
	}
	if f.ClearSkyIndex15m != 0.3 || f.SolarFraction15m != 0.15 || f.PersistenceIndex15 != 0.6 || f.ModelVersion != "fake" {
		t.Fatalf("unexpected forecast %+v", f)
	}
}

func TestServerErrorsSurface(t *testing.T) {
	c := newClient(t)
	_, err := c.Forecast(context.Background(), "n1", 13, nil)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("want InvalidArgument, got %v", err)
	}
}

func TestReady(t *testing.T) {
	ok, v, err := newClient(t).Ready(context.Background())
	if err != nil || !ok || v != "fake" {
		t.Fatalf("ready=%v version=%q err=%v", ok, v, err)
	}
}

func TestUnreachableServiceFailsFast(t *testing.T) {
	c, err := Dial("127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Timeout = 300_000_000 // 300ms
	if _, err := c.Forecast(context.Background(), "n", 12, []float64{0.5}); err == nil {
		t.Fatal("expected an error from an unreachable service")
	}
}
