package feed

import "testing"

func TestResolveFormatRHF2(t *testing.T) {
	endpoint, err := ResolveFormat("RHF2", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.Name != VendorRHF2 || endpoint.URL != RHF2DefaultListenURL {
		t.Fatalf("unexpected endpoint: %+v", endpoint)
	}

	endpoint, err = ResolveFormat("RHF2", "tcp://0.0.0.0:19770", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.URL != "tcp://0.0.0.0:19770" {
		t.Fatalf("unexpected URL: %+v", endpoint)
	}
}

func TestResolveFormatRHF2RejectsWebSocket(t *testing.T) {
	if _, err := ResolveFormat("RHF2", "ws://127.0.0.1:19770/feed", "", ""); err == nil {
		t.Fatal("expected error for ws URL")
	}
}

func TestResolveFormatNitroFeed(t *testing.T) {
	endpoint, err := ResolveFormat("NitroFeed", "ws://127.0.0.1:9642/feed", "secret", "X-User-ID")
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.Name != VendorNitroFeed || endpoint.URL != "ws://127.0.0.1:9642/feed" || endpoint.Token != "secret" || endpoint.AuthHeader != "X-User-ID" {
		t.Fatalf("unexpected endpoint: %+v", endpoint)
	}
}

func TestResolveFormatNitroFeedRequiresURL(t *testing.T) {
	if _, err := ResolveFormat("NitroFeed", "", "", ""); err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveFormatUnknown(t *testing.T) {
	if _, err := ResolveFormat("Other", "ws://x", "", ""); err == nil {
		t.Fatal("expected error")
	}
}
