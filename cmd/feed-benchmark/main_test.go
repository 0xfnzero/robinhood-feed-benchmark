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

func clearFeedEnvironment(t *testing.T) {
	t.Helper()
	for index := 1; index <= 64; index++ {
		suffix := strconv.Itoa(index)
		t.Setenv("FEED_URL_"+suffix, "")
		t.Setenv("FEED_NAME_"+suffix, "")
		t.Setenv("FEED_TOKEN_"+suffix, "")
	}
}
