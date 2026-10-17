package fix

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/biodream-llc/prova/internal/adapter"
)

func init() { adapter.Register(Adapter{}) }

// now returns a FIX UTC timestamp (YYYYMMDD-HH:MM:SS.sss).
func now() string { return time.Now().UTC().Format("20060102-15:04:05.000") }

// readMessage reads exactly one FIX message off the stream. FIX has no length-prefix frame: a
// message is delimited by parsing BeginString (8) and BodyLength (9), reading that many body bytes,
// then the fixed 7-byte CheckSum field (10=NNN<SOH>).
func readMessage(r *bufio.Reader) ([]byte, error) {
	f8, err := r.ReadBytes(0x01)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(string(f8), "8=") {
		return nil, fmt.Errorf("fix: expected BeginString, got %q", string(f8))
	}
	f9, err := r.ReadBytes(0x01)
	if err != nil {
		return nil, err
	}
	n, err := bodyLen(string(f9))
	if err != nil {
		return nil, err
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	trailer := make([]byte, 7) // "10=NNN\x01"
	if _, err := io.ReadFull(r, trailer); err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(f8)+len(f9)+len(body)+len(trailer))
	out = append(out, f8...)
	out = append(out, f9...)
	out = append(out, body...)
	out = append(out, trailer...)
	return out, nil
}

func bodyLen(f9 string) (int, error) {
	s := strings.TrimSuffix(strings.TrimPrefix(f9, "9="), soh)
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n > 1<<20 {
		return 0, fmt.Errorf("fix: bad BodyLength %q", f9)
	}
	return n, nil
}

// Adapter is the FIX protocol adapter.
type Adapter struct{}

// Name identifies the adapter.
func (Adapter) Name() string { return "fix" }

// Connect dials the venue over TCP and completes a FIX Logon handshake before returning.
func (Adapter) Connect(ctx context.Context, target adapter.Target, sess adapter.SessionConfig) (adapter.Conn, error) {
	var d net.Dialer
	nc, err := d.DialContext(ctx, "tcp", target.Addr)
	if err != nil {
		return nil, fmt.Errorf("fix: connect %s: %w", target.Addr, err)
	}
	param := func(k, dflt string) string {
		if sess.Params != nil {
			if v, ok := sess.Params[k]; ok && v != "" {
				return v
			}
		}
		return dflt
	}
	c := &conn{
		c:      nc,
		r:      bufio.NewReader(nc),
		sender: param("SenderCompID", "PROVA"),
		target: param("TargetCompID", "VENUE"),
		seq:    1,
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = nc.SetDeadline(dl)
	}
	// Logon handshake: send Logon, expect a Logon back.
	if _, err := nc.Write(Logon(c.seq, c.sender, c.target, now())); err != nil {
		_ = nc.Close()
		return nil, fmt.Errorf("fix: logon send: %w", err)
	}
	c.seq++
	raw, err := readMessage(c.r)
	if err != nil {
		_ = nc.Close()
		return nil, fmt.Errorf("fix: logon await: %w", err)
	}
	m, err := parse(raw)
	if err != nil {
		_ = nc.Close()
		return nil, err
	}
	if m[TagMsgType] != MsgLogon {
		_ = nc.Close()
		return nil, fmt.Errorf("fix: expected Logon, got MsgType %q", m[TagMsgType])
	}
	return c, nil
}

type conn struct {
	c      net.Conn
	r      *bufio.Reader
	sender string
	target string
	seq    int
	order  map[int]string
	lastTx time.Time
}

// Encode builds the message the template names (a NewOrderSingle when it names none) from the
// scenario record, and remembers it for correlation.
func (c *conn) Encode(tmpl adapter.Template, data adapter.Record) ([]byte, error) {
	req := FromRecord(data)
	if tmpl.Name != "" && tmpl.Name != "fix" {
		req.MsgType = msgType(tmpl.Name)
	}
	msg := EncodeRequest(c.seq, c.sender, c.target, now(), req)
	c.seq++
	m, err := parse(msg)
	if err != nil {
		return nil, err
	}
	c.order = m
	return msg, nil
}

func (c *conn) Send(ctx context.Context, msg []byte) (adapter.Handle, error) {
	if dl, ok := ctx.Deadline(); ok {
		_ = c.c.SetWriteDeadline(dl)
	}
	c.lastTx = time.Now()
	if _, err := c.c.Write(msg); err != nil {
		return adapter.Handle{}, fmt.Errorf("fix: send: %w", err)
	}
	id := "1"
	if c.order != nil {
		id = c.order[TagClOrdID]
	}
	return adapter.Handle{ID: id}, nil
}

// Await reads session messages, answering TestRequests with Heartbeats, until the ExecutionReport,
// OrderCancelReject or session Reject that resolves the request arrives.
func (c *conn) Await(ctx context.Context, _ adapter.Handle) (adapter.Decoded, error) {
	if dl, ok := ctx.Deadline(); ok {
		_ = c.c.SetReadDeadline(dl)
	}
	for {
		raw, err := readMessage(c.r)
		if err != nil {
			return adapter.Decoded{}, fmt.Errorf("fix: await: %w", err)
		}
		m, err := parse(raw)
		if err != nil {
			return adapter.Decoded{}, err
		}
		switch m[TagMsgType] {
		case MsgTestRequest:
			_, _ = c.c.Write(Heartbeat(c.seq, c.sender, c.target, now(), m[TagTestReqID]))
			c.seq++
			continue
		case MsgHeartbeat:
			continue
		case MsgExecutionReport, MsgReject, MsgCancelReject:
			lat := time.Since(c.lastTx)
			st, err := DecodeResponse(raw)
			if err != nil {
				return adapter.Decoded{}, err
			}
			return adapter.Decoded{
				Fields: st.Fields(),
				Raw:    raw,
				Meta:   map[string]string{"latencyMs": fmt.Sprintf("%.3f", float64(lat.Microseconds())/1000), "ordStatus": st.OrdStatus},
			}, nil
		default:
			continue
		}
	}
}

func (c *conn) Close() error { return c.c.Close() }
