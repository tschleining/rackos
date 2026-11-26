package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	requestTimeout = 10 * time.Second
	initialBackoff = time.Second
	maxBackoff     = time.Minute
)

type Client struct {
	conn    *grpc.ClientConn
	service rackosv1.AgentServiceClient
}

func New(address string, caPEM []byte) (*Client, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" {
		return nil, errors.New(
			"appliance address must contain a host and port",
		)
	}

	portNumber, err := strconv.Atoi(port)

	if err != nil ||
		portNumber < 1 ||
		portNumber > 65535 ||
		strings.ContainsAny(host, "/\\?#%@ \t\r\n") {
		return nil, errors.New("invalid appliance address")
	}

	roots := x509.NewCertPool()

	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("invalid appliance CA")
	}

	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS13,
		RootCAs:    roots,
		ServerName: host,
	}

	conn, err := grpc.NewClient(
		"passthrough:///"+address,
		grpc.WithTransportCredentials(
			credentials.NewTLS(tlsConfig),
		),
		grpc.WithDisableRetry(),
	)

	if err != nil {
		return nil, fmt.Errorf(
			"initialize appliance connection: %w", err,
		)
	}

	return &Client{
		conn:    conn,
		service: rackosv1.NewAgentServiceClient(conn),
	}, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func LocalDeviceInfo(version string) (*rackosv1.DeviceInfo, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("get hostname: %w", err)
	}

	return &rackosv1.DeviceInfo{
		Hostname:     hostname,
		Os:           runtime.GOOS,
		Architecture: runtime.GOARCH,
		AgentVersion: version,
	}, nil
}

func (c *Client) Enroll(
	ctx context.Context,
	token string,
	version string,
) (*rackosv1.EnrollResponse, error) {
	info, err := LocalDeviceInfo(version)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	ctx = withToken(ctx, token)

	return c.service.Enroll(
		ctx,
		&rackosv1.EnrollRequest{
			Device: info,
		},
	)
}

func (c *Client) Heartbeat(
	ctx context.Context,
	deviceID string,
	token string,
) (uint32, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	ctx = withToken(ctx, token)

	response, err := c.service.Heartbeat(
		ctx,
		&rackosv1.HeartbeatRequest{
			DeviceId: deviceID,
		},
	)

	if err != nil {
		return 0, err
	}

	interval := response.GetHeartbeatIntervalSeconds()

	if interval == 0 || interval > 3600 {
		return 0, status.Error(
			codes.InvalidArgument,
			"invalid heartbeat interval",
		)
	}

	return interval, nil
}

func (c *Client) Run(
	ctx context.Context,
	deviceID string,
	token string,
	logger *slog.Logger,
) error {
	backoff := initialBackoff

	for {
		if ctx.Err() != nil {
			return nil
		}

		interval, err := c.Heartbeat(
			ctx, deviceID, token,
		)

		if ctx.Err() != nil {
			return nil
		}

		var delay time.Duration

		if err == nil {
			backoff = initialBackoff
			delay = time.Duration(interval) * time.Second

			logger.Debug(
				"Heartbeat acknowledged",
				"next_heartbeat_seconds", interval,
			)
		} else {
			switch status.Code(err) {
			case codes.Unauthenticated,
				codes.PermissionDenied:
				return errors.New(
					"agent authentication rejected; administrator intervention required",
				)

			case codes.InvalidArgument,
				codes.Unimplemented,
				codes.FailedPrecondition:
				return fmt.Errorf(
					"agent protocol rejected: %s",
					status.Code(err),
				)
			}

			delay = backoff
			backoff = nextBackoff(backoff)

			logger.Warn(
				"Heartbeat failed; retrying",
				"code", status.Code(err).String(),
				"retry_in", delay.String(),
			)
		}

		if !wait(ctx, delay) {
			return nil
		}
	}
}

func withToken(
	ctx context.Context,
	token string,
) context.Context {
	return metadata.AppendToOutgoingContext(
		ctx,
		"authorization",
		"Bearer "+token,
	)
}

func nextBackoff(current time.Duration) time.Duration {
	next := current * 2

	if next > maxBackoff {
		return maxBackoff
	}

	return next
}

func wait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
