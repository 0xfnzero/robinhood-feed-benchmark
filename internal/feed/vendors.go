package feed

import (
	"fmt"
	"net/url"
	"strings"
)

const (
	VendorRHF2      = "RHF2"
	VendorNitroFeed = "NitroFeed"
	VendorDirect    = "DIRECT"
	VendorNGF1      = "NGF1"
	VendorRBH1      = "RBH1"

	RHF2DefaultListenURL   = "tcp://0.0.0.0:19770"
	DirectDefaultListenURL = "direct://0.0.0.0:19780"
	NGF1DefaultListenURL   = "ngf1://0.0.0.0:19780"
	RBH1DefaultURL         = "ws://127.0.0.1:9642/bin"
)

// ResolveFormat builds an Endpoint from a wire-format name (not a brand).
//
//	RHF2      — binary TCP push/listen; URL like tcp://0.0.0.0:19770
//	NitroFeed — Nitro JSON WebSocket; URL like ws://host/feed or wss://host/feed
//	DIRECT    — DIRECT binary TCP; URL like direct://0.0.0.0:19780
//	NGF1      — NGF1 envelope over TCP; URL like ngf1://0.0.0.0:19780
//	RBH1      — RBH1 binary (WSS /bin or TCP); URL like ws://host/bin or rbh1://host:19791
func ResolveFormat(format, feedURL, token, authHeader string) (Endpoint, error) {
	name := strings.TrimSpace(format)
	address := strings.TrimSpace(feedURL)
	credential := strings.TrimSpace(token)
	header := strings.TrimSpace(authHeader)

	switch strings.ToLower(name) {
	case "rhf2":
		if address == "" {
			address = RHF2DefaultListenURL
		}
		if err := requireURLScheme(address, "tcp", "rhf2", "tcp-listen", "rhf2-listen"); err != nil {
			return Endpoint{}, fmt.Errorf("%s: %w (example: %s)", VendorRHF2, err, RHF2DefaultListenURL)
		}
		return Endpoint{Name: VendorRHF2, URL: address}, nil

	case "direct":
		if address == "" {
			address = DirectDefaultListenURL
		}
		if err := requireURLScheme(address, "direct", "direct-listen", "tcp-direct"); err != nil {
			return Endpoint{}, fmt.Errorf("%s: %w (example: %s)", VendorDirect, err, DirectDefaultListenURL)
		}
		return Endpoint{Name: VendorDirect, URL: address}, nil

	case "ngf1":
		// Live :19780 push uses NGF1 envelope (distinct from DIRECT).
		if address == "" {
			address = NGF1DefaultListenURL
		}
		// Allow tcp:// as alias; normalize to ngf1:// for IsNGF1Endpoint.
		if strings.HasPrefix(strings.ToLower(address), "tcp://") {
			address = "ngf1://" + address[len("tcp://"):]
		}
		if err := requireURLScheme(address, "ngf1", "ngf1-listen"); err != nil {
			return Endpoint{}, fmt.Errorf("%s: %w (example: %s)", VendorNGF1, err, NGF1DefaultListenURL)
		}
		return Endpoint{Name: VendorNGF1, URL: address}, nil

	case "rbh1":
		if address == "" {
			address = RBH1DefaultURL
		}
		// Normalize bare tcp://host:port → rbh1:// so it is not treated as RHF2.
		if strings.HasPrefix(strings.ToLower(address), "tcp://") {
			address = "rbh1://" + address[len("tcp://"):]
		}
		if err := requireURLScheme(address, "ws", "wss", "rbh1", "rbh1-listen", "tcp-rbh1"); err != nil {
			return Endpoint{}, fmt.Errorf("%s: %w (example: %s or rbh1://127.0.0.1:19791)", VendorRBH1, err, RBH1DefaultURL)
		}
		endpoint := Endpoint{
			Name:       VendorRBH1,
			URL:        address,
			Token:      credential,
			AuthHeader: header,
		}
		if header == "" && (strings.HasPrefix(strings.ToLower(address), "ws://") || strings.HasPrefix(strings.ToLower(address), "wss://")) {
			endpoint.AuthHeader = "X-Token"
		}
		return ApplyHostAuth(endpoint)

	case "nitrofeed", "nitro":
		if address == "" {
			return Endpoint{}, fmt.Errorf("%s requires FEED_URL (ws:// or wss://)", VendorNitroFeed)
		}
		if err := requireURLScheme(address, "ws", "wss"); err != nil {
			return Endpoint{}, fmt.Errorf("%s: %w", VendorNitroFeed, err)
		}
		endpoint := Endpoint{
			Name:       VendorNitroFeed,
			URL:        address,
			Token:      credential,
			AuthHeader: header,
		}
		return ApplyHostAuth(endpoint)

	default:
		return Endpoint{}, fmt.Errorf("unknown feed format %q (supported: %s, %s, %s, %s, %s)", format, VendorRHF2, VendorNitroFeed, VendorDirect, VendorNGF1, VendorRBH1)
	}
}

// KnownFormat reports whether name is a built-in wire format.
func KnownFormat(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "rhf2", "nitrofeed", "nitro", "direct", "ngf1", "rbh1":
		return true
	default:
		return false
	}
}

// Deprecated aliases kept for call sites during rename.
func ResolveVendor(vendor, token string) (Endpoint, error) {
	return ResolveFormat(vendor, "", token, "")
}

func KnownVendor(name string) bool { return KnownFormat(name) }

func requireURLScheme(raw string, schemes ...string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("invalid URL %q", raw)
	}
	scheme := strings.ToLower(parsed.Scheme)
	for _, allowed := range schemes {
		if scheme == allowed {
			return nil
		}
	}
	return fmt.Errorf("URL %q must use scheme %s", raw, strings.Join(schemes, "/"))
}
