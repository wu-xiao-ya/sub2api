package service

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/imroc/req/v3"
)

func TestAccountOutboundOperationReqResponsePreventsReplay(t *testing.T) {
	installTestRelay(t)
	ctx := withAccountOperationRoute(context.Background(), &Account{UseRelayRoute: true}, "")
	calls := 0
	expected := &req.Response{Response: &http.Response{StatusCode: http.StatusOK}}
	expectedErr := errors.New("proxyconnect tcp: i/o timeout")
	response, _, err := accountOutboundOperation(ctx, "", func(string) (*req.Response, error) {
		calls++
		return expected, expectedErr
	})
	if calls != 1 || response != expected || err != expectedErr {
		t.Fatalf("response with error was replayed: calls=%d err=%v", calls, err)
	}
}

func TestAccountPrivacyOperationRetriesEmptyReqResponse(t *testing.T) {
	svc := installTestRelay(t)
	ctx := withAccountOperationRoute(context.Background(), &Account{UseRelayRoute: true}, "http://own:3128")
	var proxies []string
	factory := func(proxy string) (*req.Client, error) {
		proxies = append(proxies, proxy)
		return req.C(), nil
	}
	calls := 0
	response, err := accountPrivacyOperation(ctx, factory, "", func(*req.Client) (*req.Response, error) {
		calls++
		if calls == 1 {
			return &req.Response{}, errors.New("proxyconnect tcp: i/o timeout")
		}
		return &req.Response{Response: &http.Response{StatusCode: http.StatusOK}}, nil
	})
	if err != nil || response.StatusCode != http.StatusOK ||
		!reflect.DeepEqual(proxies, []string{svc.relaySnap.url, "http://own:3128"}) {
		t.Fatalf("calls=%v err=%v", proxies, err)
	}
}

func TestAccountOutboundOperationRetainsLoadedDefault(t *testing.T) {
	svc := installTestRelay(t)
	id := int64(5)
	account := &Account{ID: 3, ProxyID: &id, UseRelayRoute: true}
	ctx := withAccountOperationRoute(context.Background(), account, "http://loaded-default:3128")
	var calls []string
	value, used, err := accountOutboundOperation(ctx, "", func(proxy string) (string, error) {
		calls = append(calls, proxy)
		if proxy == svc.relaySnap.url {
			return "", errors.New("proxyconnect tcp: i/o timeout")
		}
		return "new-token", nil
	})
	if err != nil || value != "new-token" || used != "http://loaded-default:3128" ||
		!reflect.DeepEqual(calls, []string{svc.relaySnap.url, used}) {
		t.Fatalf("value=%q used=%q calls=%v err=%v", value, used, calls, err)
	}
	if account.Proxy != nil {
		t.Fatal("shared account was mutated")
	}
	calls = nil
	_, _, err = accountOutboundOperation(ctx, "", func(proxy string) (string, error) {
		calls = append(calls, proxy)
		return "metadata", nil
	})
	if err != nil || !reflect.DeepEqual(calls, []string{used}) {
		t.Fatalf("subsequent operation ignored cooldown: %v %v", calls, err)
	}
}

func TestAccountOutboundOperationDoesNotReplayTokenSuccessOrOriginFailure(t *testing.T) {
	installTestRelay(t)
	ctx := withAccountOperationRoute(context.Background(), &Account{UseRelayRoute: true}, "")
	for _, expectedErr := range []error{nil, errors.New("invalid_grant"), errors.New("tls: handshake failure"),
		errors.New("proxyconnect tcp: Bad Gateway")} {
		calls := 0
		_, _, err := accountOutboundOperation(ctx, "", func(string) (string, error) {
			calls++
			return "rotated-token", expectedErr
		})
		if calls != 1 || err != expectedErr {
			t.Fatalf("calls=%d err=%v expected=%v", calls, err, expectedErr)
		}
	}
}

func TestAccountOutboundOperationCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := accountOutboundOperation(ctx, "", func(string) (string, error) {
		t.Fatal("cancelled operation was called")
		return "", nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}
