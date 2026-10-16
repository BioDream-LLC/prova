// Package iso8583 implements prova's second protocol adapter: ISO 8583 card authorisation messages
// (MTI 0100 request / 0110 response). It includes a real ASCII bitmap packager — the layer every
// competitor's tooling is built around — an adapter over a length-prefixed TCP session, and a
// built-in switch simulator so scenarios run end-to-end with no external system.
package iso8583

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// field kinds.
const (
	fixed = iota
	llvar
	lllvar
)

type fieldSpec struct {
	kind   int
	length int
}

// spec is the (deliberately compact) data-element dictionary this adapter speaks. It is a genuine
// subset of ISO 8583 field encodings: fixed-length numerics plus LLVAR for the PAN.
var spec = map[int]fieldSpec{
	2:  {llvar, 19}, // Primary account number
	3:  {fixed, 6},  // Processing code
	4:  {fixed, 12}, // Amount, transaction (minor units)
	7:  {fixed, 10}, // Transmission date & time
	11: {fixed, 6},  // System trace audit number
	39: {fixed, 2},  // Response code
	41: {fixed, 8},  // Card acceptor terminal id
	49: {fixed, 3},  // Currency code, transaction (numeric)
}

// Pack encodes an MTI and a set of data elements into an ASCII ISO 8583 message: 4-char MTI, a
// 16-hex-char primary bitmap, then the present fields in numeric order.
func Pack(mti string, fields map[int]string) ([]byte, error) {
	if len(mti) != 4 {
		return nil, fmt.Errorf("iso8583: MTI must be 4 chars, got %q", mti)
	}
	des := make([]int, 0, len(fields))
	for de := range fields {
		if _, ok := spec[de]; !ok {
			return nil, fmt.Errorf("iso8583: DE%d not in packager spec", de)
		}
		des = append(des, de)
	}
	sort.Ints(des)

	var bitmap [8]byte
	for _, de := range des {
		bitmap[(de-1)/8] |= 1 << uint(7-((de-1)%8))
	}

	var body strings.Builder
	for _, de := range des {
		s := spec[de]
		v := fields[de]
		switch s.kind {
		case fixed:
			if len(v) > s.length {
				return nil, fmt.Errorf("iso8583: DE%d value too long", de)
			}
			body.WriteString(strings.Repeat("0", s.length-len(v)) + v)
		case llvar:
			if len(v) > s.length {
				return nil, fmt.Errorf("iso8583: DE%d value too long", de)
			}
			fmt.Fprintf(&body, "%02d%s", len(v), v)
		case lllvar:
			fmt.Fprintf(&body, "%03d%s", len(v), v)
		}
	}
	return []byte(mti + strings.ToUpper(fmt.Sprintf("%016x", bytesToUint64(bitmap))) + body.String()), nil
}

func bytesToUint64(b [8]byte) uint64 {
	var v uint64
	for i := 0; i < 8; i++ {
		v = v<<8 | uint64(b[i])
	}
	return v
}

// Unpack decodes an ASCII ISO 8583 message into its MTI and data elements.
func Unpack(raw []byte) (string, map[int]string, error) {
	s := string(raw)
	if len(s) < 20 {
		return "", nil, fmt.Errorf("iso8583: message too short")
	}
	mti := s[:4]
	bm, err := strconv.ParseUint(s[4:20], 16, 64)
	if err != nil {
		return "", nil, fmt.Errorf("iso8583: bad bitmap: %w", err)
	}
	fields := map[int]string{}
	pos := 20
	for bit := 2; bit <= 64; bit++ {
		if bm&(1<<uint(64-bit)) == 0 {
			continue
		}
		sp, ok := spec[bit]
		if !ok {
			return "", nil, fmt.Errorf("iso8583: DE%d present but not in spec", bit)
		}
		switch sp.kind {
		case fixed:
			if pos+sp.length > len(s) {
				return "", nil, fmt.Errorf("iso8583: truncated DE%d", bit)
			}
			fields[bit] = s[pos : pos+sp.length]
			pos += sp.length
		case llvar, lllvar:
			n := 2
			if sp.kind == lllvar {
				n = 3
			}
			if pos+n > len(s) {
				return "", nil, fmt.Errorf("iso8583: truncated length of DE%d", bit)
			}
			l, err := strconv.Atoi(s[pos : pos+n])
			if err != nil {
				return "", nil, fmt.Errorf("iso8583: bad length of DE%d", bit)
			}
			pos += n
			if pos+l > len(s) {
				return "", nil, fmt.Errorf("iso8583: truncated DE%d", bit)
			}
			fields[bit] = s[pos : pos+l]
			pos += l
		}
	}
	return mti, fields, nil
}

var currencyNum = map[string]string{"EUR": "978", "USD": "840", "GBP": "826", "JPY": "392"}

// Request is the normalized input a scenario fills to produce a 0100.
type Request struct {
	PAN            string
	ProcessingCode string
	Amount         string // major units decimal, e.g. "150.00"
	Currency       string
	STAN           string
	Terminal       string
}

// FromRecord builds a Request from a flat string record.
func FromRecord(r map[string]string) Request {
	get := func(k, dflt string) string {
		if v, ok := r[k]; ok && v != "" {
			return v
		}
		return dflt
	}
	return Request{
		PAN:            get("PAN", "4111111111111111"),
		ProcessingCode: get("ProcessingCode", "000000"),
		Amount:         get("Amount", "0.00"),
		Currency:       get("Currency", "EUR"),
		STAN:           get("STAN", "000001"),
		Terminal:       get("Terminal", "TERM0001"),
	}
}

// Encode renders a 0100 authorisation request.
func Encode(req Request) ([]byte, error) {
	ccy := currencyNum[strings.ToUpper(req.Currency)]
	if ccy == "" {
		ccy = "978"
	}
	return Pack("0100", map[int]string{
		2:  req.PAN,
		3:  req.ProcessingCode,
		4:  fmt.Sprintf("%012d", minorUnits(req.Amount)),
		11: req.STAN,
		41: req.Terminal,
		49: ccy,
	})
}

// Status is the normalized response: the response code and whether it approved.
type Status struct {
	MTI          string
	STAN         string
	ResponseCode string // DE39: 00 approved; 51 insufficient funds; etc.
}

// EncodeResponse renders a 0110 authorisation response (used by the simulated switch).
func EncodeResponse(reqFields map[int]string, de39 string) ([]byte, error) {
	return Pack("0110", map[int]string{
		3:  getDE(reqFields, 3, "000000"),
		4:  getDE(reqFields, 4, "000000000000"),
		11: getDE(reqFields, 11, "000001"),
		39: de39,
		49: getDE(reqFields, 49, "978"),
	})
}

func getDE(f map[int]string, de int, dflt string) string {
	if v, ok := f[de]; ok {
		return v
	}
	return dflt
}

// DecodeResponse decodes a 0110 into a Status.
func DecodeResponse(raw []byte) (Status, error) {
	mti, fields, err := Unpack(raw)
	if err != nil {
		return Status{}, err
	}
	return Status{MTI: mti, STAN: fields[11], ResponseCode: fields[39]}, nil
}

// Fields flattens a Status into the normalized, assertable form, including the canonical decision
// keys every verdict backend understands across protocols.
func (s Status) Fields() map[string]string {
	f := map[string]string{
		"MTI":  s.MTI,
		"DE11": s.STAN,
		"DE39": s.ResponseCode,
	}
	if s.ResponseCode == "00" {
		f["decision"] = "accepted"
	} else {
		f["decision"] = "rejected"
		f["rejectReason"] = s.ResponseCode
	}
	return f
}

// minorUnits parses "150.00" into integer minor units (15000).
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
