package feed

import "testing"

func TestApplyAuthDefaultBearer(t *testing.T) {
	ep, err := ApplyAuth(Endpoint{
		Name:  "Cloud",
		URL:   "wss://feed.example.com/feed",
		Token: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ep.AuthHeader != "" || ep.Token != "secret" || ep.URL != "wss://feed.example.com/feed" {
		t.Fatalf("expected default bearer token left intact: %+v", ep)
	}
}

func TestApplyAuthExplicitHeader(t *testing.T) {
	ep, err := ApplyAuth(Endpoint{
		Name:       "Cloud",
		URL:        "wss://feed.example.com/feed",
		Token:      "api-key",
		AuthHeader: "X-Token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ep.AuthHeader != "X-Token" || ep.Token != "api-key" {
		t.Fatalf("unexpected endpoint: %+v", ep)
	}
}

func TestApplyAuthPathStyle(t *testing.T) {
	ep, err := ApplyAuth(Endpoint{
		Name:      "Cloud",
		URL:       "wss://feed.example.com/ws",
		Token:     "tok",
		AuthStyle: "path",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ep.Token != "" || ep.URL != "wss://feed.example.com/ws/tok" {
		t.Fatalf("unexpected endpoint: %+v", ep)
	}
}

func TestApplyAuthHeaderStyleRequiresHeader(t *testing.T) {
	if _, err := ApplyAuth(Endpoint{
		Name:      "Cloud",
		URL:       "wss://feed.example.com/feed",
		Token:     "api-key",
		AuthStyle: "header",
	}); err == nil {
		t.Fatal("expected error when style=header without AuthHeader")
	}
}

func TestApplyAuthExplicitHeaderWinsOverHost(t *testing.T) {
	ep, err := ApplyAuth(Endpoint{
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

func TestApplyAuthHostHeaderInference(t *testing.T) {
	ep, err := ApplyAuth(Endpoint{
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

func TestApplyAuthHostPathInferenceWS(t *testing.T) {
	ep, err := ApplyAuth(Endpoint{
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

func TestApplyAuthHostPathInferenceFeed(t *testing.T) {
	ep, err := ApplyAuth(Endpoint{
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

func TestApplyAuthExplicitStyleWinsOverHost(t *testing.T) {
	ep, err := ApplyAuth(Endpoint{
		Name:      "Cloud",
		URL:       "wss://robinhood-ohio.flashblock.trade/v1/feed",
		Token:     "api-key",
		AuthStyle: "bearer",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ep.AuthHeader != "" || ep.Token != "api-key" {
		t.Fatalf("explicit bearer should skip host header: %+v", ep)
	}
}
