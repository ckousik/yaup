package yaup

import (
	"net/http"
	"testing"
	"time"
)

func init() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		session, err := Upgrade(w, r, nil, nil)
		if err != nil {
			panic(err)
		}
		stream, err := session.Open()
		if err != nil {
			panic(err)
		}
		_, _ = stream.Write([]byte("ping"))
	})
	go func() {
		_ = http.ListenAndServe(":8080", nil)
	}()
}

func TestClient(t *testing.T) {
	d := Dialer{HandshakeTimeout: time.Duration(10 * time.Second)}
	session, _, err := d.Dial("yamux://localhost:8080", make(http.Header))
	if err != nil {
		t.Fatal(err)
	}
	stream, err := session.Accept()
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	_, _ = stream.Read(buf)
	if string(buf) != "ping" {
		t.Fatalf("Wrong message")
	}
}
