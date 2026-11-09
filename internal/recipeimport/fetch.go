package recipeimport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// maxPageBytes is the most of a page read. Recipe pages with their
// JSON-LD are well under it, and it bounds the memory an import uses on the
// small server.
const maxPageBytes = 5 << 20

// fetchTimeout bounds how long fetching a page takes, redirects included.
const fetchTimeout = 15 * time.Second

// ErrNotPublic is returned for a URL whose host isn't a public internet
// address, like localhost or the cloud's metadata server, which the server
// must not be made to fetch.
var ErrNotPublic = errors.New("the address isn't a public web site")

// Fetcher fetches web pages.
type Fetcher interface {
	Fetch(ctx context.Context, rawURL string) ([]byte, error)
}

// HTTPFetcher fetches pages from the public internet only.
type HTTPFetcher struct {
	client *http.Client
}

// NewHTTPFetcher returns a fetcher that only connects to public addresses,
// checked on every connection, redirects included, so a page can't
// redirect it to an internal one.
func NewHTTPFetcher() *HTTPFetcher {
	dialer := &net.Dialer{
		Timeout: fetchTimeout,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if ip := net.ParseIP(host); ip == nil || !isPublic(ip) {
				return ErrNotPublic
			}
			return nil
		},
	}
	transport := &http.Transport{
		// No proxy, as one would connect to the page's host itself,
		// unchecked.
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   fetchTimeout,
		ResponseHeaderTimeout: fetchTimeout,
		MaxIdleConns:          4,
		IdleConnTimeout:       30 * time.Second,
	}
	return &HTTPFetcher{client: &http.Client{Transport: transport, Timeout: fetchTimeout}}
}

// sharedAddressSpace is carrier-grade NAT's range, which isn't public
// either, and which net.IP.IsPrivate leaves out.
var sharedAddressSpace = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// isPublic reports whether an IP address is on the public internet.
func isPublic(ip net.IP) bool {
	return !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast() && !ip.IsInterfaceLocalMulticast() &&
		!ip.IsMulticast() && !ip.IsUnspecified() && !sharedAddressSpace.Contains(ip)
}

// CheckURL checks that a URL is an http or https URL, returning it
// normalized.
func CheckURL(rawURL string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("give the page's full address, starting with https://")
	}
	u.Fragment = ""
	return u.String(), nil
}

// Fetch returns the page at a URL, at most maxPageBytes of it.
func (f *HTTPFetcher) Fetch(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	// Some sites refuse requests without a browser's headers.
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; mealplann.ing recipe import)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the site answered %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxPageBytes))
}

// Describe says why a page couldn't be fetched, for people: Go's errors
// name the request and the network calls, like `Get "https://…": dial
// tcp: lookup …: no such host`.
func Describe(err error) string {
	var dnsErr *net.DNSError
	var netErr net.Error
	switch {
	case errors.Is(err, ErrNotPublic):
		return ErrNotPublic.Error()
	case errors.As(err, &dnsErr):
		return "the site couldn't be found"
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()):
		return "the site took too long to answer"
	case strings.HasPrefix(err.Error(), "the site answered"):
		return err.Error()
	default:
		return "the site couldn't be reached"
	}
}
