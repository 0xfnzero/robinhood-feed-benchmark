package feed

import "testing"

func TestApplyHostAuthFlashblockHeader(t *testing.T) {
	ep, err := ApplyHostAuth(Endpoint{
		Name:  "Cloud",
		URL:   "wss://robinhood-ohio.flashblock.trade/v1/feed",
		Token: "api-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ep.AuthHeader != "X-User-ID" || ep.Token != "api-key" {
		t.Fatalf("unexpected endpoint: %+v", ep)
	}
}

func TestApplyHostAuthBlockrazorPath(t *testing.T) {
	ep, err := ApplyHostAuth(Endpoint{
		Name:  "Cloud",
		URL:   "wss://us.robinhood-feeder.blockrazor.io/ws",
		Token: "tok",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ep.Token != "" || ep.URL != "wss://us.robinhood-feeder.blockrazor.io/ws/tok" {
		t.Fatalf("unexpected endpoint: %+v", ep)
	}
}

func TestApplyHostAuthNode1Path(t *testing.T) {
	ep, err := ApplyHostAuth(Endpoint{
		Name:  "Cloud",
		URL:   "ws://ohio.robinhood-feeder.node1.me/feed",
		Token: "uuid-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ep.Token != "" || ep.URL != "ws://ohio.robinhood-feeder.node1.me/feed/uuid-1" {
		t.Fatalf("unexpected endpoint: %+v", ep)
	}
}

func TestApplyHostAuthExplicitHeaderWins(t *testing.T) {
	ep, err := ApplyHostAuth(Endpoint{
		Name:       "Cloud",
		URL:        "wss://robinhood-ohio.flashblock.trade/v1/feed",
		Token:      "api-key",
		AuthHeader: "X-Custom",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ep.AuthHeader != "X-Custom" {
		t.Fatalf("explicit header should win: %+v", ep)
	}
}

func TestApplyHostAuthDefaultBearer(t *testing.T) {
	ep, err := ApplyHostAuth(Endpoint{
		Name:  "Cloud",
		URL:   "wss://feed.example.com/feed",
		Token: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ep.AuthHeader != "" || ep.Token != "secret" {
		t.Fatalf("expected default bearer token left intact: %+v", ep)
	}
}
