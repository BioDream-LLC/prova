// Package fix implements prova's third protocol adapter: FIX 4.4 (Financial Information eXchange),
// the session protocol electronic trading runs on. It includes a real tag=value / SOH codec with
// genuine BodyLength (9) and CheckSum (10) computation, a session layer (Logon, heartbeats and
// per-side sequence numbers), and a built-in simulated venue so order scenarios run end-to-end with
// no external exchange. Order outcomes are normalized to the same canonical decision keys the other
// protocols emit, so the one casebook judges a FIX reject exactly as it judges an ISO 8583 decline.
package fix

import (
	"fmt"
	"strconv"
	"strings"
)

// soh is the FIX field separator (ASCII SOH, 0x01).
const soh = "\x01"

// beginString is the FIX version this adapter speaks.
const beginString = "FIX.4.4"

// Tags used by this adapter (a genuine subset of the FIX 4.4 dictionary).
const (
	TagBeginString  = 8
	TagBodyLength   = 9
	TagMsgType      = 35
	TagSenderCompID = 49
	TagTargetCompID = 56
	TagMsgSeqNum    = 34
	TagSendingTime  = 52
	TagCheckSum     = 10

	TagAccount      = 1
	TagClOrdID      = 11
	TagCumQty       = 14
	TagCurrency     = 15
	TagExecID       = 17
	TagOrderQty     = 38
	TagOrdStatus    = 39
	TagOrdType      = 40
	TagPrice        = 44
	TagSide         = 54
	TagSymbol       = 55
	TagText         = 58
	TagTimeInForce  = 59
	TagTransactTime = 60
	TagAvgPx        = 6
	TagLeavesQty    = 151
	TagOrderID      = 37
	TagExecType     = 150
	TagOrdRejReason = 103
	TagEncryptMeth  = 98
	TagHeartBtInt   = 108
	TagTestReqID    = 112
)

// MsgType values.
const (
	MsgLogon           = "A"
	MsgHeartbeat       = "0"
	MsgTestRequest     = "1"
	MsgReject          = "3"
	MsgNewOrderSingle  = "D"
	MsgExecutionReport = "8"
)

// field is a single tag/value pair; a FIX message is an ordered list of them.
type field struct {
	tag int
	val string
}

// build assembles a complete FIX message: the standard header (8,9,35,49,56,34,52), the body fields,
// and a trailer with a real CheckSum (10). BodyLength (9) counts every byte after its own SOH up to
// and including the SOH before the CheckSum, exactly as the protocol requires.
func build(msgType string, seq int, sender, target, sendingTime string, body []field) []byte {
	var sb strings.Builder
	put := func(tag int, v string) { fmt.Fprintf(&sb, "%d=%s%s", tag, v, soh) }
	put(TagMsgType, msgType)
	put(TagSenderCompID, sender)
	put(TagTargetCompID, target)
	put(TagMsgSeqNum, strconv.Itoa(seq))
	put(TagSendingTime, sendingTime)
	for _, f := range body {
		put(f.tag, f.val)
	}
	bodyStr := sb.String()
	full := fmt.Sprintf("8=%s%s9=%d%s%s", beginString, soh, len(bodyStr), soh, bodyStr)
	full += fmt.Sprintf("10=%03d%s", checksum(full), soh)
	return []byte(full)
}

// checksum is the FIX modulo-256 sum of every byte of s.
func checksum(s string) int {
	sum := 0
	for i := 0; i < len(s); i++ {
		sum += int(s[i])
	}
	return sum % 256
}

// parse decodes a raw FIX message into a tag->value map, verifying the CheckSum so a corrupt or
// truncated message is rejected rather than silently misread.
func parse(raw []byte) (map[int]string, error) {
	s := string(raw)
	i := strings.Index(s, soh+"10=")
	if i >= 0 {
		upto := s[:i+1] // through the SOH before 10=
		if i+7 <= len(s) {
			want := s[i+4 : i+7]
			if fmt.Sprintf("%03d", checksum(upto)) != want {
				return nil, fmt.Errorf("fix: checksum mismatch")
			}
		}
	}
	m := map[int]string{}
	for _, p := range strings.Split(s, soh) {
		if p == "" {
			continue
		}
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			continue
		}
		tag, err := strconv.Atoi(k)
		if err != nil {
			continue
		}
		m[tag] = v
	}
	if _, ok := m[TagMsgType]; !ok {
		return nil, fmt.Errorf("fix: message has no MsgType (35)")
	}
	return m, nil
}

// Request is the normalized order a scenario fills to produce a NewOrderSingle.
type Request struct {
	ClOrdID  string
	Account  string
	Symbol   string
	Side     string // 1 buy, 2 sell
	OrderQty string
	Price    string
	Currency string
}

// FromRecord builds a Request from a flat string record, applying defaults.
func FromRecord(r map[string]string) Request {
	get := func(k, dflt string) string {
		if v, ok := r[k]; ok && v != "" {
			return v
		}
		return dflt
	}
	return Request{
		ClOrdID:  get("ClOrdID", "ORD-1"),
		Account:  get("Account", "ACC1"),
		Symbol:   get("Symbol", "ACME"),
		Side:     side(get("Side", "buy")),
		OrderQty: get("OrderQty", "0"),
		Price:    get("Price", "0.00"),
		Currency: get("Currency", "USD"),
	}
}

func side(s string) string {
	switch strings.ToLower(s) {
	case "sell", "2":
		return "2"
	default:
		return "1"
	}
}

// Logon renders a Logon (A) message.
func Logon(seq int, sender, target, now string) []byte {
	return build(MsgLogon, seq, sender, target, now, []field{
		{TagEncryptMeth, "0"},
		{TagHeartBtInt, "30"},
	})
}

// Heartbeat renders a Heartbeat (0), optionally echoing a TestReqID.
func Heartbeat(seq int, sender, target, now, testReqID string) []byte {
	var body []field
	if testReqID != "" {
		body = append(body, field{TagTestReqID, testReqID})
	}
	return build(MsgHeartbeat, seq, sender, target, now, body)
}

// EncodeOrder renders a NewOrderSingle (D) as a limit order.
func EncodeOrder(seq int, sender, target, now string, req Request) []byte {
	return build(MsgNewOrderSingle, seq, sender, target, now, []field{
		{TagClOrdID, req.ClOrdID},
		{TagAccount, req.Account},
		{TagSymbol, req.Symbol},
		{TagSide, req.Side},
		{TagTransactTime, now},
		{TagOrderQty, req.OrderQty},
		{TagOrdType, "2"}, // limit
		{TagPrice, req.Price},
		{TagCurrency, req.Currency},
		{TagTimeInForce, "0"}, // day
	})
}

// ExecFill renders an ExecutionReport (8) reporting a full fill.
func ExecFill(seq int, sender, target, now string, order map[int]string, execID string) []byte {
	qty := order[TagOrderQty]
	return build(MsgExecutionReport, seq, sender, target, now, []field{
		{TagOrderID, "V-" + execID},
		{TagClOrdID, order[TagClOrdID]},
		{TagExecID, execID},
		{TagExecType, "F"},  // trade
		{TagOrdStatus, "2"}, // filled
		{TagSymbol, order[TagSymbol]},
		{TagSide, order[TagSide]},
		{TagOrderQty, qty},
		{TagCumQty, qty},
		{TagLeavesQty, "0"},
		{TagAvgPx, order[TagPrice]},
		{TagPrice, order[TagPrice]},
	})
}

// ExecReject renders an ExecutionReport (8) reporting a rejected order with an OrdRejReason (103).
func ExecReject(seq int, sender, target, now string, order map[int]string, execID, rejReason, text string) []byte {
	return build(MsgExecutionReport, seq, sender, target, now, []field{
		{TagOrderID, "V-" + execID},
		{TagClOrdID, order[TagClOrdID]},
		{TagExecID, execID},
		{TagExecType, "8"},  // rejected
		{TagOrdStatus, "8"}, // rejected
		{TagSymbol, order[TagSymbol]},
		{TagSide, order[TagSide]},
		{TagOrderQty, order[TagOrderQty]},
		{TagCumQty, "0"},
		{TagLeavesQty, "0"},
		{TagOrdRejReason, rejReason},
		{TagText, text},
	})
}

// Status is the normalized response leg: the message type and order status fields.
type Status struct {
	MsgType      string
	ClOrdID      string
	OrdStatus    string
	ExecType     string
	OrdRejReason string
	Text         string
}

// DecodeResponse decodes an ExecutionReport or Reject into a Status.
func DecodeResponse(raw []byte) (Status, error) {
	m, err := parse(raw)
	if err != nil {
		return Status{}, err
	}
	return Status{
		MsgType:      m[TagMsgType],
		ClOrdID:      m[TagClOrdID],
		OrdStatus:    m[TagOrdStatus],
		ExecType:     m[TagExecType],
		OrdRejReason: m[TagOrdRejReason],
		Text:         m[TagText],
	}, nil
}

// Fields flattens a Status into the normalized, assertable form, including the canonical decision
// keys every verdict backend understands across protocols. A Reject (35=3) or a rejected
// ExecutionReport (39=8 / 150=8) is a canonical rejection carrying its OrdRejReason.
func (s Status) Fields() map[string]string {
	f := map[string]string{
		"35":  s.MsgType,
		"11":  s.ClOrdID,
		"39":  s.OrdStatus,
		"150": s.ExecType,
	}
	rejected := s.MsgType == MsgReject || s.OrdStatus == "8" || s.ExecType == "8"
	if rejected {
		f["decision"] = "rejected"
		reason := s.OrdRejReason
		if reason == "" {
			reason = "REJECT"
		}
		f["rejectReason"] = reason
		if s.Text != "" {
			f["103text"] = s.Text
		}
	} else {
		f["decision"] = "accepted"
	}
	return f
}

// notionalMinor returns price*qty in integer minor units, the economic size the venue checks against
// buying power. price is a decimal string ("10.00"); qty is an integer string ("15").
func notionalMinor(price, qty string) int64 {
	q, _ := strconv.ParseInt(strings.TrimSpace(qty), 10, 64)
	return minorUnits(price) * q
}

// minorUnits parses "10.00" into integer minor units (1000).
func minorUnits(s string) int64 {
	s = strings.TrimSpace(s)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimLeft(s, "+-")
	whole, frac, _ := strings.Cut(s, ".")
	frac = (frac + "00")[:2]
	w, _ := strconv.ParseInt(whole, 10, 64)
	fr, _ := strconv.ParseInt(frac, 10, 64)
	v := w*100 + fr
	if neg {
		v = -v
	}
	return v
}
