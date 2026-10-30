package fix

import (
	"fmt"
	"strconv"
	"strings"
)

// Session-layer and version support beyond the order messages: FIX 4.2 sessions, session Rejects and
// Logouts, possible-duplicate resends, the order mass cancel, and the deliberate faults prova's session
// scenarios send.

// More FIX tags.
const (
	TagExecTransType          = 20
	TagPossDupFlag            = 43
	TagRefSeqNum              = 45
	TagOrigSendingTime        = 122
	TagRefTagID               = 371
	TagRefMsgType             = 372
	TagSessionRejectReason    = 373
	TagMassCancelRequestType  = 530
	TagMassCancelResponse     = 531
	TagMassCancelRejectReason = 532
	TagTotalAffectedOrders    = 533
)

// More MsgTypes.
const (
	MsgMassCancelRequest = "q"
	MsgMassCancelReport  = "r"
)

// BeginString42 is the FIX 4.2 session version.
const BeginString42 = "FIX.4.2"

// SessionRejectReason (373) values prova's venue sends.
const (
	RejectRequiredTagMissing = "1"
	RejectValueIncorrect     = "5"
	RejectCompIDProblem      = "9"
	RejectInvalidMsgType     = "11"
)

// header tags are written by buildWith itself; parseOrdered leaves them out of the body.
var headerTags = map[int]bool{TagBeginString: true, TagBodyLength: true, TagMsgType: true, TagSenderCompID: true,
	TagTargetCompID: true, TagMsgSeqNum: true, TagSendingTime: true, TagCheckSum: true}

// parseOrdered splits a message into its header values and its body fields in order.
func parseOrdered(raw []byte) (map[int]string, []field) {
	hdr := map[int]string{}
	var body []field
	for _, p := range strings.Split(string(raw), soh) {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			continue
		}
		tag, err := strconv.Atoi(k)
		if err != nil {
			continue
		}
		if headerTags[tag] {
			hdr[tag] = v
			continue
		}
		body = append(body, field{tag, v})
	}
	return hdr, body
}

// Rebrand rewrites a message for a session version: the BeginString, and for FIX 4.2 the execution
// report fields that version uses (ExecType 2 for a fill rather than 4.4's F, and ExecTransType 20).
// BodyLength and CheckSum are recomputed.
func Rebrand(msg []byte, bs string) []byte {
	if bs == "" || bs == BeginString44 {
		return msg
	}
	hdr, body := parseOrdered(msg)
	seq, _ := strconv.Atoi(hdr[TagMsgSeqNum])
	if bs == BeginString42 && hdr[TagMsgType] == MsgExecutionReport {
		var out []field
		for _, f := range body {
			if f.tag == TagExecType && f.val == "F" {
				f.val = "2"
			}
			out = append(out, f)
			if f.tag == TagExecID {
				out = append(out, field{TagExecTransType, "0"})
			}
		}
		body = out
	}
	return buildWith(bs, hdr[TagMsgType], seq, hdr[TagSenderCompID], hdr[TagTargetCompID], hdr[TagSendingTime], body)
}

// WithPossDup marks a message as a possible duplicate (43=Y), with its original sending time (122)
// unless origTime is empty, and re-frames it under the same sequence number.
func WithPossDup(msg []byte, origTime string) []byte {
	hdr, body := parseOrdered(msg)
	seq, _ := strconv.Atoi(hdr[TagMsgSeqNum])
	extra := []field{{TagPossDupFlag, "Y"}}
	if origTime != "" {
		extra = append(extra, field{TagOrigSendingTime, origTime})
	}
	return buildWith(beginOf(msg), hdr[TagMsgType], seq, hdr[TagSenderCompID], hdr[TagTargetCompID], hdr[TagSendingTime], append(extra, body...))
}

// WithSeq re-frames a message under another sequence number.
func WithSeq(msg []byte, seq int) []byte {
	hdr, body := parseOrdered(msg)
	return buildWith(beginOf(msg), hdr[TagMsgType], seq, hdr[TagSenderCompID], hdr[TagTargetCompID], hdr[TagSendingTime], body)
}

// CorruptChecksum returns the message with a wrong CheckSum, as line noise would leave it.
func CorruptChecksum(msg []byte) []byte {
	s := string(msg)
	i := strings.LastIndex(s, soh+"10=")
	if i < 0 || i+7 > len(s) {
		return msg
	}
	n, _ := strconv.Atoi(s[i+4 : i+7])
	return []byte(fmt.Sprintf("%s10=%03d%s", s[:i+1], (n+1)%256, soh))
}

func beginOf(msg []byte) string {
	s := string(msg)
	if strings.HasPrefix(s, "8=") {
		if i := strings.Index(s, soh); i > 2 {
			return s[2:i]
		}
	}
	return BeginString44
}

// SessionReject renders a session-level Reject (3) of the message with sequence refSeq.
func SessionReject(bs string, seq int, sender, target, now string, refSeq int, refTag, refMsgType, reason, text string) []byte {
	body := []field{{TagRefSeqNum, strconv.Itoa(refSeq)}}
	if refTag != "" {
		body = append(body, field{TagRefTagID, refTag})
	}
	if refMsgType != "" {
		body = append(body, field{TagRefMsgType, refMsgType})
	}
	body = append(body, field{TagSessionRejectReason, reason}, field{TagText, text})
	return buildWith(bs, MsgReject, seq, sender, target, now, body)
}

// Logout renders a Logout (5) with a reason text.
func Logout(bs string, seq int, sender, target, now, text string) []byte {
	return buildWith(bs, MsgLogout, seq, sender, target, now, []field{{TagText, text}})
}

// EncodeMassCancel renders an OrderMassCancelRequest (q): cancel every open order (530=7), or every open
// order in one instrument (530=1) when symbol is set.
func EncodeMassCancel(seq int, sender, target, now string, req Request) []byte {
	body := []field{{TagClOrdID, req.ClOrdID}}
	if req.Symbol != "" && req.Symbol != "*" {
		body = append(body, field{TagMassCancelRequestType, "1"}, field{TagSymbol, req.Symbol})
	} else {
		body = append(body, field{TagMassCancelRequestType, "7"})
	}
	body = append(body, field{TagTransactTime, now})
	return build(MsgMassCancelRequest, seq, sender, target, now, body)
}

// MassCancelReport renders an OrderMassCancelReport (r): accepted with the request type and the number
// of orders cancelled, or refused (531=0) with a MassCancelRejectReason (532).
func MassCancelReport(seq int, sender, target, now string, req map[int]string, response, rejectReason string, affected int) []byte {
	body := []field{{TagClOrdID, req[TagClOrdID]}, {TagOrderID, "MC-" + req[TagClOrdID]}, {TagMassCancelRequestType, req[TagMassCancelRequestType]},
		{TagMassCancelResponse, response}}
	if rejectReason != "" {
		body = append(body, field{TagMassCancelRejectReason, rejectReason})
	}
	body = append(body, field{TagTotalAffectedOrders, strconv.Itoa(affected)})
	if s := req[TagSymbol]; s != "" {
		body = append(body, field{TagSymbol, s})
	}
	return build(MsgMassCancelReport, seq, sender, target, now, body)
}
