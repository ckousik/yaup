package yaup

import (
	"bufio"
	"fmt"
	"github.com/hashicorp/yamux"
	// "log"
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

	//Jar ...
	Jar http.CookieJar
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

	var deadline time.Time
	if d.HandshakeTimeout != time.Duration(0) {
		deadline = time.Now().Add(d.HandshakeTimeout)
	}

	dial := (&net.Dialer{Deadline: deadline}).Dial
	conn, err := dial("tcp", addPort(u.Host))
	if err != nil {
		return nil, nil, err
	}
	// close connection if something goes wrong at the end
	defer func() {
		if conn != nil {
			_ = conn.Close()
		}
	}()
	// Write request over connection
	if err = req.Write(conn); err != nil {
		return nil, nil, err
	}
	// Open server
	session, err := yamux.Server(conn, nil)
	if err != nil {
		return nil, nil, err
	}

	connReader := bufio.NewReader(conn)
	res, err := http.ReadResponse(connReader, nil)
	if err != nil {
		_ = session.Close()
		return nil, nil, err
	}
	if res.Header.Get("Upgrade") != "yamux" {
		_ = session.Close()
		return nil, nil, ErrBadHandshake
	}
	conn = nil
	return session, nil, nil
}
