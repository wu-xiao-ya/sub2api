package usagesource

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const (
	Header          = "X-Sub2API-Usage-Source"
	SignatureHeader = "X-Sub2API-Usage-Source-Token"
	ChannelMonitor  = "channel_monitor"
)

var internalToken = newInternalToken()

func Normalize(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case ChannelMonitor:
		return ChannelMonitor, true
	default:
		return "", false
	}
}

func NormalizeVerified(rawSource, rawToken string) (string, bool) {
	source, ok := Normalize(rawSource)
	if !ok {
		return "", false
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(rawToken)), []byte(internalToken)) != 1 {
		return "", false
	}
	return source, true
}

func MarkChannelMonitor(headers map[string]string) map[string]string {
	if headers == nil {
		headers = make(map[string]string, 2)
	}
	headers[Header] = ChannelMonitor
	headers[SignatureHeader] = internalToken
	return headers
}

func SetChannelMonitor(headerSetter interface{ Set(string, string) }) {
	if headerSetter == nil {
		return
	}
	headerSetter.Set(Header, ChannelMonitor)
	headerSetter.Set(SignatureHeader, internalToken)
}

func newInternalToken() string {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err == nil {
		return hex.EncodeToString(token)
	}
	return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
}
