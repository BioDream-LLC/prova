package main

import (
	"fmt"
	"strings"

	"github.com/biodream-llc/prova/internal/fix"
	"github.com/biodream-llc/prova/internal/iso20022"
	"github.com/biodream-llc/prova/internal/iso8583"
)

// counterpartyReply composes the wire response a counterparty (payment host, card switch or trading
// venue) would send for a chosen decision, and returns it both as raw bytes and as the normalized,
// assertable fields. It lets a user act as the counterparty in the browser: dictate the reply and see
// exactly what goes on the wire and how prova reads it. It reuses the same encoders the simulators do.
func counterpartyReply(protocol, message, decision, reason string) (string, map[string]string, error) {
	accept := !strings.EqualFold(decision, "reject")
	switch protocol {
	case "iso20022", "":
		st := iso20022.Status{Kind: iso20022.KindPacs002, MsgId: "RSP-CP", OrgnlEndToEndId: "E2E-CP"}
		if accept {
			st.TxSts = "ACSP"
		} else {
			st.TxSts, st.ReasonCode = "RJCT", orDefaultCode(reason, "AM04")
		}
		raw, err := iso20022.EncodeStatus(st)
		if err != nil {
			return "", nil, err
		}
		return string(raw), st.Fields(), nil
	case "iso8583":
		de39 := "00"
		if !accept {
			de39 = orDefaultCode(reason, "51")
		}
		mti := "0100"
		if message != "" {
			mti = message
		}
		reqFields := map[int]string{3: "000000", 4: "000009000", 11: "000001", 49: "978"}
		raw, err := iso8583.EncodeResponse(mti, reqFields, de39)
		if err != nil {
			return "", nil, err
		}
		st, err := iso8583.DecodeResponse(raw)
		if err != nil {
			return "", nil, err
		}
		return string(raw), st.Fields(), nil
	case "fix":
		order := map[int]string{fix.TagClOrdID: "ORD-CP", fix.TagSymbol: "ACME", fix.TagSide: "1", fix.TagOrderQty: "10", fix.TagPrice: "10.00"}
		var raw []byte
		if accept {
			raw = fix.ExecFill(2, "VENUE", "PROVA", "20260101-00:00:00.000", order, "000001")
		} else {
			raw = fix.ExecReject(2, "VENUE", "PROVA", "20260101-00:00:00.000", order, "000001", orDefaultCode(reason, "3"), "rejected by counterparty")
		}
		st, err := fix.DecodeResponse(raw)
		if err != nil {
			return "", nil, err
		}
		return string(raw), st.Fields(), nil
	}
	return "", nil, fmt.Errorf("unknown protocol %q", protocol)
}

func orDefaultCode(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}
