package rackosv1

import (
	"testing"

	"google.golang.org/protobuf/proto"
)

func TestEnrollRequestRoundTrip(t *testing.T) {
	original := &EnrollRequest{
		Device: &DeviceInfo{
			Hostname:     "server-01",
			Os:           "linux",
			Architecture: "amd64",
			AgentVersion: "0.1.0-dev",
		},
	}

	data, err := proto.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}

	decoded := &EnrollRequest{}

	if err := proto.Unmarshal(data, decoded); err != nil {
		t.Fatal(err)
	}

	if !proto.Equal(original, decoded) {
		t.Fatal("decoded message differs from original")
	}
}

func TestHeartbeatRequestRoundTrip(t *testing.T) {
	original := &HeartbeatRequest{
		DeviceId: "e2a104d8-9c72-4b31-8fa2-63001b24c9e8",
	}

	data, err := proto.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}

	decoded := &HeartbeatRequest{}

	if err := proto.Unmarshal(data, decoded); err != nil {
		t.Fatal(err)
	}

	if !proto.Equal(original, decoded) {
		t.Fatal("decoded heartbeat differs from original")
	}
}

func TestAgentServiceContract(t *testing.T) {
	if AgentService_ServiceDesc.ServiceName != "rackos.v1.AgentService" {
		t.Fatal("unexpected agent service name")
	}

	expected := map[string]bool{
		"Enroll":    false,
		"Heartbeat": false,
	}

	for _, method := range AgentService_ServiceDesc.Methods {
		if _, ok := expected[method.MethodName]; ok {
			expected[method.MethodName] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("missing RPC method: %s", name)
		}
	}
}
