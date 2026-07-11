package gateway

import (
	"strings"
	"testing"
)

func TestAggregateInstallAllSucceed(t *testing.T) {
	resp := aggregateInstall([]installOutcome{
		{addr: "10.0.0.1:8080", status: "INSTALLED"},
		{addr: "10.0.0.2:8080", status: "INSTALLED"},
	})
	if !resp.GetSuccess() {
		t.Fatalf("expected success, got %+v", resp)
	}
	if resp.GetError() != "" {
		t.Errorf("expected no error, got %q", resp.GetError())
	}
	if resp.GetStatus() != "INSTALLED" {
		t.Errorf("status = %q, want INSTALLED", resp.GetStatus())
	}
}

func TestAggregateInstallPartialFailure(t *testing.T) {
	resp := aggregateInstall([]installOutcome{
		{addr: "10.0.0.1:8080", status: "INSTALLED"},
		{addr: "10.0.0.2:8080", status: "FAILED", fail: "go build error"},
		{addr: "10.0.0.3:8080", fail: "connection refused"},
	})
	if resp.GetSuccess() {
		t.Fatal("expected failure when any instance fails")
	}
	// Every failing instance must be named, the succeeding one must not.
	for _, want := range []string{"10.0.0.2:8080", "go build error", "10.0.0.3:8080", "connection refused"} {
		if !strings.Contains(resp.GetError(), want) {
			t.Errorf("error %q missing %q", resp.GetError(), want)
		}
	}
	if strings.Contains(resp.GetError(), "10.0.0.1:8080") {
		t.Errorf("succeeding instance should not appear in error: %q", resp.GetError())
	}
}
