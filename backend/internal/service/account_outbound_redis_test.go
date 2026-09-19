package service

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestRelayRedisOutageAcrossProcessesAndExpiry(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	first := &AccountOutboundService{rdb: client}
	second := &AccountOutboundService{rdb: client}
	route := AccountOutbound{ProxyURL: "http://relay:38480", unavailableTTL: 45 * time.Second}
	first.MarkDown(context.Background(), route)
	if !second.isDown(context.Background(), route.ProxyURL) {
		t.Fatal("outage was not shared")
	}
	if second.isDown(context.Background(), "http://another:38480") {
		t.Fatal("outage crossed relay configurations")
	}
	if ttl := server.TTL(relayDownKey(route.ProxyURL)); ttl != 45*time.Second {
		t.Fatalf("unexpected TTL %s", ttl)
	}
	short := route
	short.unavailableTTL = 5 * time.Second
	second.MarkDown(context.Background(), short)
	if ttl := server.TTL(relayDownKey(route.ProxyURL)); ttl != 45*time.Second {
		t.Fatalf("a late failure shortened the outage: %s", ttl)
	}
	server.FastForward(46 * time.Second)
	third := &AccountOutboundService{rdb: client}
	if third.isDown(context.Background(), route.ProxyURL) {
		t.Fatal("expired outage still active")
	}
}

func TestRelayRedisFailureKeepsLocalCooldown(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1,
		DialTimeout: 50 * time.Millisecond, ReadTimeout: 50 * time.Millisecond, WriteTimeout: 50 * time.Millisecond})
	t.Cleanup(func() { _ = client.Close() })
	server.Close()
	svc := &AccountOutboundService{rdb: client}
	route := AccountOutbound{ProxyURL: "http://relay:38480"}
	svc.MarkDown(context.Background(), route)
	if !svc.isDown(context.Background(), route.ProxyURL) {
		t.Fatal("Redis failure erased local cooldown")
	}
}
