package feed

import (
	"fmt"
	"net/url"
	"strings"
)

// ApplyHostAuth maps a generic auth token onto provider-specific transport
// details inferred from the feed URL host. Display names stay brand-agnostic;
// only the hostname is inspected.
//
// Styles currently recognized:
//   - *.flashblock.trade  → request header X-User-ID
//   - *.blockrazor.io     → append token as /ws/<token> path segment
//   - *.node1.me          → append token as /feed/<token> or /tx/<token>
//   - other hosts         → Authorization: Bearer <token> (default dial behavior)
func ApplyHostAuth(endpoint Endpoint) (Endpoint, error) {
	token := strings.TrimSpace(endpoint.Token)
	if token == "" {
		return endpoint, nil
	}
	// Explicit header from the user wins over host inference.
	if strings.TrimSpace(endpoint.AuthHeader) != "" {
		return endpoint, nil
	}

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
		next, err := appendPathToken(parsed, "/ws", token)
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

func hostHasSuffix(host, suffix string) bool {
	host = strings.TrimSuffix(host, ".")
	return host == suffix || strings.HasSuffix(host, "."+suffix)
}

func appendPathToken(parsed *url.URL, marker, token string) (string, error) {
	path := strings.TrimSuffix(parsed.EscapedPath(), "/")
	marker = strings.TrimSuffix(marker, "/")
	if path == "" {
		path = marker
	}
	if !strings.HasPrefix(path, marker) && path != marker {
		// Keep existing path, but ensure marker prefix when path is empty-ish.
		if path == "" || path == "/" {
			path = marker
		}
	}
	// Already has a non-empty segment after /ws
	if strings.HasPrefix(path, marker+"/") {
		rest := strings.TrimPrefix(path, marker+"/")
		if rest != "" && rest != token {
			// Token already present (or a different path token); do not rewrite.
			return parsed.String(), nil
		}
		if rest == token {
			return parsed.String(), nil
		}
	}
	if path == marker || path == marker+"/" {
		parsed.Path = marker + "/" + token
		parsed.RawPath = ""
		return parsed.String(), nil
	}
	// Path like /ws/<something> already handled; if path doesn't look like /ws, append.
	if strings.HasPrefix(path, marker) {
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
		// Already has a credential segment.
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
