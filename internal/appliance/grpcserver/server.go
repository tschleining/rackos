package grpcserver

import (
	"context"
	"fmt"
	"net"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

type Server struct {
	server *grpc.Server
	health *health.Server
}

type agentService struct {
	rackosv1.UnimplementedAgentServiceServer
}

func New() *Server {
	server := grpc.NewServer(
		grpc.MaxRecvMsgSize(1<<20),
		grpc.MaxSendMsgSize(1<<20),
	)

	rackosv1.RegisterAgentServiceServer(
		server,
		&agentService{},
	)

	healthServer := health.NewServer()

	healthpb.RegisterHealthServer(
		server,
		healthServer,
	)

	healthServer.SetServingStatus(
		"",
		healthpb.HealthCheckResponse_SERVING,
	)

	healthServer.SetServingStatus(
		rackosv1.AgentService_ServiceDesc.ServiceName,
		healthpb.HealthCheckResponse_NOT_SERVING,
	)

	return &Server{
		server: server,
		health: healthServer,
	}
}

func Listen(addr string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid gRPC address: %w", err)
	}

	ip := net.ParseIP(host)

	if ip == nil || !ip.IsLoopback() {
		return nil, fmt.Errorf(
			"gRPC requires a loopback IP address until TLS is enabled",
		)
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen on gRPC address: %w", err)
	}

	return listener, nil
}

func (s *Server) Serve(listener net.Listener) error {
	return s.server.Serve(listener)
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.health.Shutdown()

	done := make(chan struct{})

	go func() {
		s.server.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
		return nil

	case <-ctx.Done():
		s.server.Stop()
		return fmt.Errorf(
			"gRPC shutdown deadline exceeded: %w",
			ctx.Err(),
		)
	}
}
