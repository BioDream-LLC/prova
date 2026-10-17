package iso20022

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

// maxFrame caps a single message to guard against a hostile or confused peer.
const maxFrame = 4 << 20 // 4 MiB

// writeFrame writes a 4-byte big-endian length prefix followed by the payload.
func writeFrame(w io.Writer, b []byte) error {
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(b)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err := w.Write(b)
	return err
}

// readFrame reads one length-prefixed frame.
func readFrame(r io.Reader) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n == 0 || n > maxFrame {
		return nil, fmt.Errorf("iso20022: bad frame length %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// Adapter is the ISO 20022 protocol adapter.
type Adapter struct{}

// Name identifies the adapter in scenarios and the registry.
func (Adapter) Name() string { return "iso20022" }

// Connect dials the target over TCP and returns a synchronous request/response session.
func (Adapter) Connect(ctx context.Context, target adapter.Target, _ adapter.SessionConfig) (adapter.Conn, error) {
	var d net.Dialer
	c, err := d.DialContext(ctx, "tcp", target.Addr)
	if err != nil {
		return nil, fmt.Errorf("iso20022: connect %s: %w", target.Addr, err)
	}
	return &conn{c: c, r: bufio.NewReader(c)}, nil
}

type conn struct {
	c      net.Conn
	r      *bufio.Reader
	lastTx time.Time
}

// Encode turns a data record into the ISO 20022 message the template names (pacs.008 when it names
// none): pacs.008, pacs.009, pacs.004, camt.056 or pacs.028.
func (c *conn) Encode(tmpl adapter.Template, data adapter.Record) ([]byte, error) {
	req := FromRecord(data)
	if tmpl.Name != "" && tmpl.Name != "iso20022" {
		req.Kind = tmpl.Name
	}
	return Encode(req)
}

// Send writes the framed message and records the send time for latency.
func (c *conn) Send(ctx context.Context, msg []byte) (adapter.Handle, error) {
	if dl, ok := ctx.Deadline(); ok {
		_ = c.c.SetWriteDeadline(dl)
	}
	c.lastTx = time.Now()
	if err := writeFrame(c.c, msg); err != nil {
		return adapter.Handle{}, fmt.Errorf("iso20022: send: %w", err)
	}
	return adapter.Handle{ID: "1"}, nil
}

// Await reads the next framed response, decodes the pacs.002 (or camt.029) and normalizes it.
func (c *conn) Await(ctx context.Context, _ adapter.Handle) (adapter.Decoded, error) {
	if dl, ok := ctx.Deadline(); ok {
		_ = c.c.SetReadDeadline(dl)
	}
	raw, err := readFrame(c.r)
	if err != nil {
		return adapter.Decoded{}, fmt.Errorf("iso20022: await: %w", err)
	}
	lat := time.Since(c.lastTx)
	st, err := DecodeStatus(raw)
	if err != nil {
		return adapter.Decoded{}, err
	}
	return adapter.Decoded{
		Fields: st.Fields(),
		Raw:    raw,
		Meta: map[string]string{
			"latencyMs": fmt.Sprintf("%.3f", float64(lat.Microseconds())/1000),
			"txSts":     st.TxSts,
		},
	}, nil
}

// Close ends the session.
func (c *conn) Close() error { return c.c.Close() }
