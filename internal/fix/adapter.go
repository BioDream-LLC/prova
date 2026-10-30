package fix

import (
	"bufio"
	"context"
	"errors"
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
		bs:     param("BeginString", BeginString44),
		seq:    1,
	}
	appl := param("DefaultApplVerID", "")
	if c.bs == BeginStringT11 && appl == "" {
		appl = ApplVer50SP2
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = nc.SetDeadline(dl)
	}
	// Logon handshake: send Logon, expect a Logon back.
	if _, err := nc.Write(LogonV(c.bs, appl, c.seq, c.sender, c.target, now())); err != nil {
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
	bs     string // the session's BeginString: FIX.4.2, FIX.4.4 or FIXT.1.1
	seq    int
	order  map[int]string
	lastTx time.Time
	// before is a message sent ahead of the scenario's own, whose reply is read and set aside (the
	// original of a possible-duplicate resend).
	before []byte
	// quiet means silence is a valid answer: a compliant venue ignores a garbled message or a duplicate.
	quiet bool
}

// The session scenarios: a NewOrderSingle sent with one deliberate session fault.
const (
	SessionSeqTooLow     = "session:seq-too-low"     // a MsgSeqNum already used, without PossDupFlag: the venue must log out
	SessionPossDupNoTime = "session:possdup-no-time" // PossDupFlag=Y without OrigSendingTime: Reject 373=1
	SessionPossDupReplay = "session:possdup-replay"  // a processed order resent as a possible duplicate: ignored
	SessionWrongCompID   = "session:wrong-comp-id"   // TargetCompID is not the venue's: Reject 373=9 and Logout
	SessionBadChecksum   = "session:bad-checksum"    // a garbled CheckSum: the message must be ignored
)

// Encode builds the message the template names (a NewOrderSingle when it names none) from the
// scenario record, in the session's version, and remembers it for correlation.
func (c *conn) Encode(tmpl adapter.Template, data adapter.Record) ([]byte, error) {
	req := FromRecord(data)
	if tmpl.Name != "" && tmpl.Name != "fix" {
		req.MsgType = msgType(tmpl.Name)
	}
	kind := req.MsgType
	if strings.HasPrefix(kind, "session:") {
		req.MsgType = MsgNewOrderSingle
	}
	target := c.target
	if kind == SessionWrongCompID {
		target = "NOT-" + c.target
	}
	t := now()
	msg := Rebrand(EncodeRequest(c.seq, c.sender, target, t, req), c.bs)
	c.seq++
	c.before, c.quiet = nil, false
	switch kind {
	case SessionSeqTooLow:
		msg = WithSeq(msg, 1) // 1 was the Logon's
	case SessionPossDupNoTime:
		msg = WithPossDup(msg, "")
	case SessionPossDupReplay:
		c.before = msg
		msg = WithPossDup(msg, t)
		c.quiet = true
	case SessionBadChecksum:
		msg = CorruptChecksum(msg)
		c.quiet = true
	case SessionWrongCompID, "":
	}
	m := parseLax(msg)
	if m[TagMsgType] == "" {
		return nil, fmt.Errorf("fix: encoded message has no MsgType")
	}
	c.order = m
	return msg, nil
}

func (c *conn) Send(ctx context.Context, msg []byte) (adapter.Handle, error) {
	if dl, ok := ctx.Deadline(); ok {
		_ = c.c.SetWriteDeadline(dl)
	}
	if c.before != nil {
		// Send the original and set its execution report aside before the resend.
		if _, err := c.c.Write(c.before); err != nil {
			return adapter.Handle{}, fmt.Errorf("fix: send: %w", err)
		}
		if _, err := c.await(ctx, false); err != nil {
			return adapter.Handle{}, fmt.Errorf("fix: the original before the resend: %w", err)
		}
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

// quietWindow is how long prova waits for a venue to answer a message it should ignore.
const quietWindow = 400 * time.Millisecond

// Await reads session messages, answering TestRequests with Heartbeats, until the ExecutionReport,
// OrderCancelReject, OrderMassCancelReport, session Reject or Logout that resolves the request arrives.
// When silence is the right answer (a garbled message, a duplicate) and none comes within a short
// window, the result is a refusal with reason IGNORED.
func (c *conn) Await(ctx context.Context, _ adapter.Handle) (adapter.Decoded, error) {
	return c.await(ctx, c.quiet)
}

func (c *conn) await(ctx context.Context, quiet bool) (adapter.Decoded, error) {
	if dl, ok := ctx.Deadline(); ok {
		_ = c.c.SetReadDeadline(dl)
	}
	if quiet {
		_ = c.c.SetReadDeadline(time.Now().Add(quietWindow))
	}
	for {
		raw, err := readMessage(c.r)
		if err != nil {
			var ne net.Error
			if quiet && (errors.As(err, &ne) && ne.Timeout() || errors.Is(err, io.EOF)) {
				return adapter.Decoded{
					Fields: map[string]string{"decision": "rejected", "rejectReason": "IGNORED", "35": "(none)"},
					Meta:   map[string]string{"latencyMs": fmt.Sprintf("%.3f", float64(time.Since(c.lastTx).Microseconds())/1000)},
				}, nil
			}
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
		case MsgExecutionReport, MsgReject, MsgCancelReject, MsgMassCancelReport, MsgLogout:
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
