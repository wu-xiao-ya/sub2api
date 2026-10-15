package usagesource

import "testing"

func TestNormalizeVerifiedRequiresInternalToken(t *testing.T) {
	if _, ok := NormalizeVerified(ChannelMonitor, ""); ok {
		t.Fatal("source without internal token should not be accepted")
	}

	headers := MarkChannelMonitor(nil)
	source, ok := NormalizeVerified(headers[Header], headers[SignatureHeader])
	if !ok {
		t.Fatal("source with internal token should be accepted")
	}
	if source != ChannelMonitor {
		t.Fatalf("source = %q, want %q", source, ChannelMonitor)
	}
}
