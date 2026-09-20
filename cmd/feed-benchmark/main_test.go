package main

import (
	"strconv"
	"testing"

	"github.com/0xfnzero/robinhood-feed-benchmark/internal/feed"
)

func TestBuildEndpointsKeepsOfficialFirst(t *testing.T) {
	clearFeedEnvironment(t)
	options := options{
		official: true, officialName: "Official", officialURL: feed.OfficialMainnetURL,
		feedValues: []string{"Custom=wss://custom.example.com"},
	}
	endpoints, err := buildEndpoints(options)
	if err != nil {
		t.Fatal(err)
	}
	if len(endpoints) != 2 || endpoints[0].Name != "Official" || endpoints[1].Name != "Custom" {
		t.Fatalf("unexpected endpoint order: %+v", endpoints)
	}
}

func TestBuildEndpointsLoadsEnvironmentToken(t *testing.T) {
	clearFeedEnvironment(t)
	t.Setenv("FEED_URL_1", "wss://custom.example.com")
	t.Setenv("FEED_NAME_1", "Vendor")
	t.Setenv("FEED_TOKEN_1", "secret")
	endpoints, err := buildEndpoints(options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(endpoints) != 1 || endpoints[0].Token != "secret" {
		t.Fatalf("unexpected endpoints: %+v", endpoints)
	}
}

func TestBuildEndpointsResolvesFormatsFromEnv(t *testing.T) {
	clearFeedEnvironment(t)
	t.Setenv("FEED_VENDOR_1", "RHF2")
	t.Setenv("FEED_URL_1", "tcp://0.0.0.0:19770")
	t.Setenv("FEED_VENDOR_2", "NitroFeed")
	t.Setenv("FEED_NAME_2", "Ours")
	t.Setenv("FEED_URL_2", "ws://127.0.0.1:9642/feed")

	endpoints, err := buildEndpoints(options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(endpoints) != 2 {
		t.Fatalf("unexpected endpoints: %+v", endpoints)
	}
	if endpoints[0].Name != feed.VendorRHF2 || endpoints[0].URL != "tcp://0.0.0.0:19770" {
		t.Fatalf("unexpected RHF2 endpoint: %+v", endpoints[0])
	}
	if endpoints[1].Name != "Ours" || endpoints[1].URL != "ws://127.0.0.1:9642/feed" {
		t.Fatalf("unexpected NitroFeed endpoint: %+v", endpoints[1])
	}
}

func TestBuildEndpointsNitroFeedRequiresURL(t *testing.T) {
	clearFeedEnvironment(t)
	t.Setenv("FEED_VENDOR_1", "NitroFeed")
	if _, err := buildEndpoints(options{}); err == nil {
		t.Fatal("expected error for missing URL")
	}
}

func clearFeedEnvironment(t *testing.T) {
	t.Helper()
	for index := 1; index <= 64; index++ {
		suffix := strconv.Itoa(index)
		t.Setenv("FEED_URL_"+suffix, "")
		t.Setenv("FEED_NAME_"+suffix, "")
		t.Setenv("FEED_TOKEN_"+suffix, "")
		t.Setenv("FEED_AUTH_HEADER_"+suffix, "")
		t.Setenv("FEED_VENDOR_"+suffix, "")
	}
}
