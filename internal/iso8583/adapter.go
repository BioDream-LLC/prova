package iso8583

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/biodream-llc/prova/internal/adapter"
)

func init() { adapter.Register(Adapter{}) }

const maxFrame = 1 << 20

// writeFrame writes a 2-byte big-endian length prefix then the payload (common ISO 8583/TCP framing).
func writeFrame(w io.Writer, b []byte) error {
	var hdr [2]byte
	binary.BigEndian.PutUint16(hdr[:], uint16(len(b)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err := w.Write(b)
	return err
}

func readFrame(r io.Reader) ([]byte, error) {
	var hdr [2]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint16(hdr[:])
	if n == 0 || int(n) > maxFrame {
		return nil, fmt.Errorf("iso8583: bad frame length %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// Adapter is the ISO 8583 protocol adapter.
type Adapter struct{}

// Name identifies the adapter.
func (Adapter) Name() string { return "iso8583" }

// Connect dials the target over TCP.
func (Adapter) Connect(ctx context.Context, target adapter.Target, _ adapter.SessionConfig) (adapter.Conn, error) {
	var d net.Dialer
	c, err := d.DialContext(ctx, "tcp", target.Addr)
	if err != nil {
		return nil, fmt.Errorf("iso8583: connect %s: %w", target.Addr, err)
	}
	return &conn{c: c, r: bufio.NewReader(c)}, nil
}

type conn struct {
	c      net.Conn
	r      *bufio.Reader
	lastTx time.Time
}

func (c *conn) Encode(_ adapter.Template, data adapter.Record) ([]byte, error) {
	return Encode(FromRecord(data))
}

func (c *conn) Send(ctx context.Context, msg []byte) (adapter.Handle, error) {
	if dl, ok := ctx.Deadline(); ok {
		_ = c.c.SetWriteDeadline(dl)
	}
	c.lastTx = time.Now()
	if err := writeFrame(c.c, msg); err != nil {
		return adapter.Handle{}, fmt.Errorf("iso8583: send: %w", err)
	}
	return adapter.Handle{ID: "1"}, nil
}

func (c *conn) Await(ctx context.Context, _ adapter.Handle) (adapter.Decoded, error) {
	if dl, ok := ctx.Deadline(); ok {
		_ = c.c.SetReadDeadline(dl)
	}
	raw, err := readFrame(c.r)
	if err != nil {
		return adapter.Decoded{}, fmt.Errorf("iso8583: await: %w", err)
	}
	lat := time.Since(c.lastTx)
	st, err := DecodeResponse(raw)
	if err != nil {
		return adapter.Decoded{}, err
	}
	return adapter.Decoded{
		Fields: st.Fields(),
		Raw:    raw,
		Meta:   map[string]string{"latencyMs": fmt.Sprintf("%.3f", float64(lat.Microseconds())/1000), "de39": st.ResponseCode},
	}, nil
}

func (c *conn) Close() error { return c.c.Close() }
