package grpcserver

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strconv"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
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

func New(
	tlsConfig *tls.Config,
	service rackosv1.AgentServiceServer,
) *Server {
	server := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(tlsConfig)),
		grpc.MaxRecvMsgSize(1<<20),
		grpc.MaxSendMsgSize(1<<20),
	)

	agentStatus := healthpb.HealthCheckResponse_NOT_SERVING

	if service == nil {
		service = &agentService{}
	} else {
		agentStatus = healthpb.HealthCheckResponse_SERVING
	}

	rackosv1.RegisterAgentServiceServer(
		server,
		service,
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
		agentStatus,
	)

	return &Server{
		server: server,
		health: healthServer,
	}
}

// BindIP validates an explicit gRPC listening address.
func BindIP(addr string) (net.IP, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf(
			"invalid gRPC address: %w", err,
		)
	}

	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 0 || portNumber > 65535 {
		return nil, fmt.Errorf("invalid gRPC port")
	}

	ip := net.ParseIP(host)

	if ip == nil {
		return nil, fmt.Errorf(
			"gRPC listening address must be an explicit IP",
		)
	}

	if !ip.IsLoopback() && !ip.IsGlobalUnicast() {
		return nil, fmt.Errorf(
			"gRPC requires a loopback or unicast IP address",
		)
	}

	return ip, nil
}

func Listen(addr string) (net.Listener, error) {
	if _, err := BindIP(addr); err != nil {
		return nil, err
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf(
			"listen on gRPC address: %w", err,
		)
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
