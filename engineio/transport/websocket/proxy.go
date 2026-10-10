package websocket

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

// netDial returns the dial function of ws.Dialer for a dial to u, or nil when
// Transport.NetDial and Transport.Proxy are both unset and the default dialer
// applies. gobwas/ws has no proxy support, so Proxy is a thin CONNECT wrapper
// around NetDial.
func (t *Transport) netDial(u *url.URL) func(ctx context.Context, network, addr string) (net.Conn, error) {
	if t.NetDial == nil && t.Proxy == nil {
		return nil
	}
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		if t.NetDial != nil {
			return t.NetDial(network, addr)
		}
		var d net.Dialer
		return d.DialContext(ctx, network, addr)
	}
	if t.Proxy == nil {
		return dial
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		// The proxy chooser sees the request an HTTP client would send.
		target := *u
		switch target.Scheme {
		case "ws":
			target.Scheme = "http"
		case "wss":
			target.Scheme = "https"
		}
		proxy, err := t.Proxy(&http.Request{Method: http.MethodGet, URL: &target, Host: target.Host, Header: make(http.Header)})
		if err != nil {
			return nil, err
		}
		if proxy == nil {
			return dial(ctx, network, addr)
		}
		return connectViaProxy(ctx, dial, proxy, network, addr)
	}
}

// bufferedConn returns the bytes the CONNECT response parser read ahead before
// reading from the connection again.
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c bufferedConn) Read(p []byte) (int, error) { return c.r.Read(p) }

// connectViaProxy tunnels to addr through an http proxy with CONNECT.
func connectViaProxy(ctx context.Context, dial func(context.Context, string, string) (net.Conn, error), proxy *url.URL, network, addr string) (net.Conn, error) {
	if proxy.Scheme != "http" {
		return nil, fmt.Errorf("websocket: unsupported proxy scheme %q", proxy.Scheme)
	}
	proxyAddr := proxy.Host
	if proxy.Port() == "" {
		proxyAddr = net.JoinHostPort(proxy.Hostname(), "80")
	}
	conn, err := dial(ctx, network, proxyAddr)
	if err != nil {
		return nil, err
	}
	// Bound the exchange by the dial context.
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
		defer func() { _ = conn.SetDeadline(time.Time{}) }()
	}
	req := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Opaque: addr},
		Host:   addr,
		Header: make(http.Header),
	}
	if user := proxy.User; user != nil {
		password, _ := user.Password()
		cred := base64.StdEncoding.EncodeToString([]byte(user.Username() + ":" + password))
		req.Header.Set("Proxy-Authorization", "Basic "+cred)
	}
	if err := req.Write(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		_ = conn.Close()
		return nil, fmt.Errorf("websocket: proxy refused CONNECT to %s: %s", addr, resp.Status)
	}
	if br.Buffered() > 0 {
		return bufferedConn{Conn: conn, r: br}, nil
	}
	return conn, nil
}
