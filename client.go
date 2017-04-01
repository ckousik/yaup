package yaup

import (
	"bufio"
	"fmt"
	"github.com/hashicorp/yamux"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	// ErrHTTPSNotSupported ...
	ErrHTTPSNotSupported = fmt.Errorf("HTTPS not supported yet")

	// ErrMalformedURL ...
	ErrMalformedURL = fmt.Errorf("malformed yamux url")

	// ErrNoDuplicateHeaders ...
	ErrNoDuplicateHeaders = fmt.Errorf("no duplicate headers")

	// ErrBadHandshake ...
	ErrBadHandshake = fmt.Errorf("bad handshake")

	// ErrHandshakeTimeout ...
	ErrHandshakeTimeout = fmt.Errorf("handshake timed out")

	// DefaultTimeout ...
	DefaultTimeout = 10 * time.Second

	// DefaultTCPTimeout ...
	DefaultTCPTimeout = 3 * time.Minute
)

func parseURL(str string) (*url.URL, error) {
	u := &url.URL{}
	if strings.HasPrefix(str, "yamux://") {
		u.Scheme = "yamux"
		str = str[len("yamux://"):]
	} else {
		return nil, ErrMalformedURL
	}
	if i := strings.Index(str, "?"); i >= 0 {
		u.RawQuery = str[i+1:]
		str = str[:i]
	}
	if i := strings.Index(str, "/"); i >= 0 {
		u.Opaque = str[i:]
		str = str[:i]
	} else {
		u.Opaque = "/"
	}
	if strings.Index(str, "@") >= 0 {
		return nil, ErrMalformedURL
	}
	u.Host = str
	return u, nil

}

func addPort(host string) string {
	if i := strings.LastIndex(host, ":"); i < 0 {
		// Since we only support http
		host = host + ":80"
	}
	return host
}

/*
Dialer ...
*/
type Dialer struct {
	// HandshakeTimeout ...
	HandshakeTimeout time.Duration

	// TCPTimeout ...
	TCPTimeout time.Duration

	// Config ...
	Config *yamux.Config

	//Jar ...
	Jar http.CookieJar
}

type sessionResponse struct {
	s *yamux.Session
	r *http.Response
}

func (d *Dialer) generateRequest(u *url.URL, header http.Header) (*http.Request, error) {
	if u.Scheme != "http" {
		return nil, fmt.Errorf("Invalid scheme")
	}
	req := &http.Request{
		Method:     "GET",
		URL:        u,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Host:       u.Host,
	}

	req.Header = make(http.Header)
	req.Header["Upgrade"] = []string{"yamux"}
	req.Header["Connection"] = []string{"Upgrade"}

	// Forward headers
	for k, v := range header {
		if k == "Upgrade" || k == "Connection" {
			return nil, ErrNoDuplicateHeaders
		}
		req.Header[k] = v
	}
	if d.Jar != nil {
		for _, cookie := range d.Jar.Cookies(u) {
			req.AddCookie(cookie)
		}
	}
	return req, nil
}

func (d *Dialer) verifyResponse(res *http.Response) bool {
	if res.StatusCode != 101 ||
		res.ProtoMajor != 1 ||
		res.ProtoMinor != 1 ||
		res.Header.Get("Upgrade") != "yamux" ||
		res.Header.Get("Connection") != "Upgrade" {
		return false
	}
	return true
}

func (d *Dialer) establishSession(req *http.Request, done chan sessionResponse, errChan chan error) {
	deadline := time.Now().Add(d.TCPTimeout)
	if d.TCPTimeout == 0 {
		deadline = time.Now().Add(DefaultTCPTimeout)
	}
	dial := (&net.Dialer{Deadline: deadline}).Dial
	conn, err := dial("tcp", addPort(req.URL.Host))
	if err != nil {
		errChan <- err
		return
	}

	defer func() {
		if conn != nil {
			_ = conn.Close()
		}
	}()

	if err = req.Write(conn); err != nil {
		errChan <- err
		return
	}

	session, err := yamux.Server(conn, d.Config)
	if err != nil {
		errChan <- err
		return
	}

	stream, err := session.Accept()
	if err != nil {
		errChan <- err
		return
	}

	sr := bufio.NewReader(stream)
	res, err := http.ReadResponse(sr, req)
	if err != nil {
		errChan <- err
		return
	}
	if !d.verifyResponse(res) {
		errChan <- ErrBadHandshake
		return
	}
	conn = nil
	done <- sessionResponse{s: session, r: res}
}

// Dial ...
func (d *Dialer) Dial(urlStr string, header http.Header) (*yamux.Session, *http.Response, error) {
	u, err := parseURL(urlStr)
	if err != nil {
		return nil, nil, err
	}
	u.Scheme = "http"
	req, err := d.generateRequest(u, header)
	if err != nil {
		return nil, nil, err
	}

	done := make(chan sessionResponse)
	ec := make(chan error)

	if d.HandshakeTimeout == 0 {
		d.HandshakeTimeout = DefaultTimeout
	}

	go d.establishSession(req, done, ec)

	select {
	case sres := <-done:
		return sres.s, sres.r, nil
	case err = <-ec:
		return nil, nil, err
	case <-time.After(d.HandshakeTimeout):
		return nil, nil, ErrHandshakeTimeout
	}
}
