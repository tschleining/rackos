package agentservice

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"
	"github.com/tschleining/rackos/internal/appliance/credentials"
	"github.com/tschleining/rackos/internal/appliance/devices"
	"github.com/tschleining/rackos/internal/appliance/enrollment"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	HeartbeatIntervalSeconds = 30
	requestTimeout           = 5 * time.Second
)

type Service struct {
	rackosv1.UnimplementedAgentServiceServer

	db             *sql.DB
	repository     *devices.Repository
	installationID string
}

func New(
	db *sql.DB,
	installationID string,
) *Service {
	return &Service{
		db:             db,
		repository:     devices.NewRepository(db),
		installationID: installationID,
	}
}

func (s *Service) Enroll(
	ctx context.Context,
	req *rackosv1.EnrollRequest,
) (*rackosv1.EnrollResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	token, err := bearerToken(ctx)
	if err != nil {
		return nil, err
	}

	info, err := validateDeviceInfo(req.GetDevice())
	if err != nil {
		return nil, err
	}

	deviceID, err := generateDeviceID()
	if err != nil {
		return nil, internalError(ctx)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, internalError(ctx)
	}
	defer tx.Rollback()

	if err := enrollment.Consume(ctx, tx, token); err != nil {
		if errors.Is(err, enrollment.ErrInvalidToken) {
			return nil, status.Error(
				codes.Unauthenticated,
				"invalid enrollment credential",
			)
		}

		return nil, internalError(ctx)
	}

	repository := s.repository.WithTx(tx)

	if _, err := repository.Create(
		ctx, deviceID, info,
	); err != nil {
		if errors.Is(err, devices.ErrInvalidDevice) {
			return nil, status.Error(
				codes.InvalidArgument,
				"invalid device information",
			)
		}

		return nil, internalError(ctx)
	}

	agentToken, err := credentials.Issue(
		ctx, tx, deviceID,
	)
	if err != nil {
		return nil, internalError(ctx)
	}

	if err := tx.Commit(); err != nil {
		return nil, internalError(ctx)
	}

	return &rackosv1.EnrollResponse{
		DeviceId:                 deviceID,
		InstallationId:           s.installationID,
		HeartbeatIntervalSeconds: HeartbeatIntervalSeconds,
		AgentToken:               agentToken,
	}, nil
}

func (s *Service) Heartbeat(
	ctx context.Context,
	req *rackosv1.HeartbeatRequest,
) (*rackosv1.HeartbeatResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	token, err := bearerToken(ctx)
	if err != nil {
		return nil, err
	}

	deviceID := req.GetDeviceId()

	if deviceID == "" {
		return nil, status.Error(
			codes.Unauthenticated,
			"invalid agent credentials",
		)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, internalError(ctx)
	}
	defer tx.Rollback()

	if err := credentials.Authenticate(
		ctx, tx, deviceID, token,
	); err != nil {
		if errors.Is(err, credentials.ErrUnauthenticated) {
			return nil, status.Error(
				codes.Unauthenticated,
				"invalid agent credentials",
			)
		}

		return nil, internalError(ctx)
	}

	repository := s.repository.WithTx(tx)

	if err := repository.Heartbeat(
		ctx, deviceID,
	); err != nil {
		return nil, internalError(ctx)
	}

	if err := tx.Commit(); err != nil {
		return nil, internalError(ctx)
	}

	return &rackosv1.HeartbeatResponse{
		HeartbeatIntervalSeconds: HeartbeatIntervalSeconds,
	}, nil
}

func bearerToken(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)

	if !ok {
		return "", status.Error(
			codes.Unauthenticated,
			"missing authorization",
		)
	}

	values := md.Get("authorization")

	if len(values) != 1 {
		return "", status.Error(
			codes.Unauthenticated,
			"invalid authorization",
		)
	}

	scheme, token, found := strings.Cut(values[0], " ")

	if !found ||
		!strings.EqualFold(scheme, "Bearer") ||
		token == "" ||
		len(token) > 128 {
		return "", status.Error(
			codes.Unauthenticated,
			"invalid authorization",
		)
	}

	return token, nil
}

func validateDeviceInfo(
	info *rackosv1.DeviceInfo,
) (devices.DeviceInfo, error) {
	if info == nil {
		return devices.DeviceInfo{}, status.Error(
			codes.InvalidArgument,
			"device information is required",
		)
	}

	device := devices.DeviceInfo{
		Hostname:     strings.TrimSpace(info.GetHostname()),
		OS:           strings.TrimSpace(info.GetOs()),
		Architecture: strings.TrimSpace(info.GetArchitecture()),
		AgentVersion: strings.TrimSpace(info.GetAgentVersion()),
	}

	fields := []struct {
		value string
		max   int
	}{
		{device.Hostname, 255},
		{device.OS, 64},
		{device.Architecture, 64},
		{device.AgentVersion, 64},
	}

	for _, field := range fields {
		if field.value == "" || len(field.value) > field.max {
			return devices.DeviceInfo{}, status.Error(
				codes.InvalidArgument,
				"invalid device information",
			)
		}
	}

	return device, nil
}

func generateDeviceID() (string, error) {
	var b [16]byte

	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}

	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	return fmt.Sprintf(
		"%x-%x-%x-%x-%x",
		b[0:4],
		b[4:6],
		b[6:8],
		b[8:10],
		b[10:16],
	), nil
}

func internalError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return status.FromContextError(err).Err()
	}

	return status.Error(
		codes.Internal,
		"agent operation failed",
	)
}
