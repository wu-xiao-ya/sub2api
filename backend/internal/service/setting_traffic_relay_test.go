package service

import (
	"context"
	"testing"
)

type relaySettingsProxyRepo struct {
	ProxyRepository
	proxy *Proxy
}

func (r relaySettingsProxyRepo) GetByID(context.Context, int64) (*Proxy, error) {
	return r.proxy, nil
}

func TestTrafficRelaySettingsValidation(t *testing.T) {
	for _, test := range []struct {
		name     string
		settings SystemSettings
		proxy    *Proxy
		valid    bool
	}{
		{"disabled defaults", SystemSettings{}, nil, true},
		{"disabled deleted proxy", SystemSettings{TrafficRelayProxyID: 1}, nil, true},
		{"enabled missing proxy", SystemSettings{TrafficRelayEnabled: true}, nil, false},
		{"negative id", SystemSettings{TrafficRelayProxyID: -1}, nil, false},
		{"short ttl", SystemSettings{TrafficRelayUnavailableTTLSeconds: 1}, nil, false},
		{"long ttl", SystemSettings{TrafficRelayUnavailableTTLSeconds: 601}, nil, false},
		{"http", SystemSettings{TrafficRelayEnabled: true, TrafficRelayProxyID: 1}, &Proxy{Protocol: "http", Status: "active"}, true},
		{"socks", SystemSettings{TrafficRelayEnabled: true, TrafficRelayProxyID: 1}, &Proxy{Protocol: "socks5", Status: "active"}, false},
		{"disabled proxy", SystemSettings{TrafficRelayEnabled: true, TrafficRelayProxyID: 1}, &Proxy{Protocol: "http", Status: "inactive"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc := &SettingService{proxyRepo: relaySettingsProxyRepo{proxy: test.proxy}}
			err := svc.validateTrafficRelay(context.Background(), &test.settings)
			if (err == nil) != test.valid {
				t.Fatalf("err=%v valid=%v", err, test.valid)
			}
		})
	}
}
