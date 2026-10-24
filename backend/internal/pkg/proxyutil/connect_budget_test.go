package proxyutil

import (
	"context"
	"testing"
	"time"
)

func TestRelayConnectBudgetScope(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	marked := WithRelayConnectHost(parent, "relay:38480")
	for _, host := range []string{"relay:38480", "own-proxy:8080"} {
		ctx, stop := RelayConnectContext(marked, host)
		deadline, ok := ctx.Deadline()
		stop()
		if !ok {
			t.Fatal("missing deadline")
		}
		remaining := time.Until(deadline)
		if host == "relay:38480" && (remaining > 5*time.Second || remaining < 4*time.Second) {
			t.Fatalf("relay budget: %v", remaining)
		}
		if host != "relay:38480" && remaining < 19*time.Second {
			t.Fatalf("default proxy deadline was shortened: %v", remaining)
		}
	}
}

func TestRelayConnectBudgetLeavesFallbackTime(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ctx, stop := RelayConnectContext(WithRelayConnectHost(parent, "relay"), "relay")
	defer stop()
	<-ctx.Done()
	if parent.Err() != nil {
		t.Fatal("relay exhausted the parent deadline")
	}
}
