package feed

import (
	"fmt"
	"net/url"
	"strings"
)

// ApplyAuth applies a credential to an endpoint.
//
// Priority:
//  1. Explicit AuthHeader → dial sends <AuthHeader>: <token>
//  2. Explicit AuthStyle (bearer|header|path) → use that style
//  3. Otherwise infer from FEED_URL hostname for known transports
//  4. Default → Authorization: Bearer <token>
//
// Host inference is a convenience so users can set FEED_AUTH_TOKEN alone.
// Explicit FEED_AUTH_HEADER / FEED_AUTH_STYLE always win.
// ApplyHostAuth is a compatibility alias.
func ApplyAuth(endpoint Endpoint) (Endpoint, error) {
	token := strings.TrimSpace(endpoint.Token)
	header := strings.TrimSpace(endpoint.AuthHeader)
	style := strings.ToLower(strings.TrimSpace(endpoint.AuthStyle))

	if header != "" {
		endpoint.AuthHeader = header
		return endpoint, nil
	}
	if token == "" {
		return endpoint, nil
	}

	// Explicit style skips host inference.
	if style != "" {
		return applyAuthStyle(endpoint, style, token)
	}

	// Convenience: map known hosts → transport style (no brand display names).
	parsed, err := url.Parse(endpoint.URL)
	if err != nil || parsed.Host == "" {
		return endpoint, fmt.Errorf("invalid feed URL %q", endpoint.URL)
	}
	host := strings.ToLower(parsed.Hostname())

	switch {
	case hostHasSuffix(host, "flashblock.trade"):
		endpoint.AuthHeader = "X-User-ID"
		return endpoint, nil

	case hostHasSuffix(host, "blockrazor.io"):
		next, err := appendPathTokenAtMarker(parsed, "/ws", token)
		if err != nil {
			return endpoint, err
		}
		endpoint.URL = next
		endpoint.Token = ""
		return endpoint, nil

	case hostHasSuffix(host, "node1.me"):
		next, err := appendNodePathToken(parsed, token)
		if err != nil {
			return endpoint, err
		}
		endpoint.URL = next
		endpoint.Token = ""
		return endpoint, nil

	default:
		// Leave Token set; dial() sends Authorization: Bearer by default.
		return endpoint, nil
	}
}

// ApplyHostAuth is a compatibility alias for ApplyAuth.
func ApplyHostAuth(endpoint Endpoint) (Endpoint, error) {
	return ApplyAuth(endpoint)
}

func applyAuthStyle(endpoint Endpoint, style, token string) (Endpoint, error) {
	switch style {
	case "bearer":
		return endpoint, nil
	case "header":
		return endpoint, fmt.Errorf("auth style %q requires AuthHeader (FEED_AUTH_HEADER / --feed-auth-header)", style)
	case "path":
		parsed, err := url.Parse(endpoint.URL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return endpoint, fmt.Errorf("invalid feed URL %q", endpoint.URL)
		}
		next, err := appendPathToken(parsed, token)
		if err != nil {
			return endpoint, err
		}
		endpoint.URL = next
		endpoint.Token = ""
		return endpoint, nil
	default:
		return endpoint, fmt.Errorf("unknown auth style %q (use bearer, header, or path)", style)
	}
}

func hostHasSuffix(host, suffix string) bool {
	host = strings.TrimSuffix(host, ".")
	return host == suffix || strings.HasSuffix(host, "."+suffix)
}

func appendPathToken(parsed *url.URL, token string) (string, error) {
	path := strings.TrimSuffix(parsed.EscapedPath(), "/")
	if strings.HasSuffix(path, "/"+token) || path == "/"+token {
		return parsed.String(), nil
	}
	parsed.Path = path + "/" + token
	parsed.RawPath = ""
	return parsed.String(), nil
}

func appendPathTokenAtMarker(parsed *url.URL, marker, token string) (string, error) {
	path := strings.TrimSuffix(parsed.EscapedPath(), "/")
	marker = strings.TrimSuffix(marker, "/")
	if path == "" || path == "/" {
		path = marker
	}
	if strings.HasPrefix(path, marker+"/") {
		rest := strings.TrimPrefix(path, marker+"/")
		if rest != "" {
			return parsed.String(), nil
		}
	}
	if path == marker || path == marker+"/" || strings.HasPrefix(path, marker) {
		parsed.Path = marker + "/" + token
		parsed.RawPath = ""
		return parsed.String(), nil
	}
	parsed.Path = strings.TrimSuffix(path, "/") + "/" + token
	parsed.RawPath = ""
	return parsed.String(), nil
}

func appendNodePathToken(parsed *url.URL, token string) (string, error) {
	path := strings.TrimSuffix(parsed.EscapedPath(), "/")
	lower := strings.ToLower(path)
	switch {
	case lower == "" || lower == "/":
		parsed.Path = "/feed/" + token
	case strings.HasSuffix(lower, "/feed") || lower == "/feed":
		parsed.Path = trimPathToBase(path, "/feed") + "/" + token
	case strings.HasSuffix(lower, "/tx") || lower == "/tx":
		parsed.Path = trimPathToBase(path, "/tx") + "/" + token
	case strings.Contains(lower, "/feed/") || strings.Contains(lower, "/tx/"):
		return parsed.String(), nil
	default:
		parsed.Path = path + "/" + token
	}
	parsed.RawPath = ""
	return parsed.String(), nil
}

func trimPathToBase(path, base string) string {
	lower := strings.ToLower(path)
	idx := strings.LastIndex(lower, base)
	if idx < 0 {
		return path
	}
	return path[:idx+len(base)]
}
