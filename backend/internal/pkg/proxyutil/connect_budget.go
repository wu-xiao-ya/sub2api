package proxyutil

import (
	"context"
	"time"
)

type relayConnectHostKey struct{}

type relayConnectTarget struct {
	host     string
	deadline time.Time
}

// WithRelayConnectHost limits only this relay's TCP/CONNECT phase. Origin TLS
// and response streaming retain the original request lifetime.
func WithRelayConnectHost(ctx context.Context, host string) context.Context {
	deadline, _ := ctx.Deadline()
	return context.WithValue(ctx, relayConnectHostKey{}, relayConnectTarget{host: host, deadline: deadline})
}

func isRelayConnectHost(ctx context.Context, host string) bool {
	target, _ := ctx.Value(relayConnectHostKey{}).(relayConnectTarget)
	return target.host != "" && target.host == host
}

func RelayConnectContext(ctx context.Context, host string) (context.Context, context.CancelFunc) {
	target, _ := ctx.Value(relayConnectHostKey{}).(relayConnectTarget)
	if !isRelayConnectHost(ctx, host) {
		return context.WithCancel(ctx)
	}
	budget := 5 * time.Second
	// net/http detaches dial cancellation and deadlines for pooled connections.
	// Keep the original deadline as a value so the relay still leaves retry time.
	if !target.deadline.IsZero() {
		if remaining := time.Until(target.deadline) / 2; remaining < budget {
			budget = remaining
		}
	}
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline) / 2; remaining < budget {
			budget = remaining
		}
	}
	return context.WithTimeout(ctx, budget)
}
