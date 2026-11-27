package rackosv1

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestReportPerformanceUnimplemented(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)

	server := grpc.NewServer()

	RegisterAgentServiceServer(
		server,
		&UnimplementedAgentServiceServer{},
	)

	go func() {
		_ = server.Serve(listener)
	}()

	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithTransportCredentials(
			insecure.NewCredentials(),
		),
		grpc.WithContextDialer(
			func(
				ctx context.Context,
				_ string,
			) (net.Conn, error) {
				return listener.DialContext(ctx)
			},
		),
	)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = conn.Close()
	})

	client := NewAgentServiceClient(conn)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	response, err := client.ReportPerformance(
		ctx,
		&ReportPerformanceRequest{
			DeviceId: "e2a104d8-9c72-4b31-8fa2-63001b24c9e8",
			SampleId: "7a91d0c4e8524b36a39f186d027c5e81",
			Snapshot: testPerformanceSnapshot(),
		},
	)

	if status.Code(err) != codes.Unimplemented {
		t.Fatalf(
			"expected Unimplemented, got %v",
			err,
		)
	}

	if response != nil {
		t.Fatal("unimplemented RPC returned acknowledgement")
	}
}
