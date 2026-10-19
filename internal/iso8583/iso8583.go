// Package iso8583 implements prova's second protocol adapter: ISO 8583 card messages — authorisation
// (0100/0110), financial transaction (0200/0210), reversal (0400/0410) and network management
// (0800/0810). It includes a real ASCII bitmap packager with primary and secondary bitmaps — the layer
// every competitor's tooling is built around — an adapter over a length-prefixed TCP session, and a
// built-in switch simulator so scenarios run end-to-end with no external system.
package iso8583

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
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
	2:  {llvar, 19},   // Primary account number
	3:  {fixed, 6},    // Processing code
	4:  {fixed, 12},   // Amount, transaction (minor units)
	7:  {fixed, 10},   // Transmission date & time
	11: {fixed, 6},    // System trace audit number
	14: {fixed, 4},    // Date, expiration (YYMM)
	22: {fixed, 3},    // POS entry mode (e.g. 051 chip, 071 contactless, 010 manual)
	37: {fixed, 12},   // Retrieval reference number
	38: {fixed, 6},    // Authorisation identification response
	39: {fixed, 2},    // Response code
	41: {fixed, 8},    // Card acceptor terminal id
	49: {fixed, 3},    // Currency code, transaction (numeric)
	52: {llvar, 32},   // PIN data (ISO 9564 PIN block, 8 bytes as 16 hex; length-preserved here)
	55: {lllvar, 999}, // ICC / EMV data (BER-TLV, as hex)
	64: {llvar, 32},   // Message authentication code (MAC, 8 bytes as 16 hex; length-preserved here)
	70: {fixed, 3},    // Network management information code (secondary bitmap)
	90: {fixed, 42},   // Original data elements (secondary bitmap)
}

// Pack encodes an MTI and a set of data elements into an ASCII ISO 8583 message: 4-char MTI, a
// 16-hex-char primary bitmap (and, when any element above 64 is present, bit 1 set and a 16-hex-char
// secondary bitmap), then the present fields in numeric order.
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

	var bitmap [16]byte
	for _, de := range des {
		bitmap[(de-1)/8] |= 1 << uint(7-((de-1)%8))
		if de > 64 {
			bitmap[0] |= 0x80 // bit 1: a secondary bitmap follows
		}
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
	bm := strings.ToUpper(fmt.Sprintf("%016x", bytesToUint64(bitmap[:8])))
	if bitmap[0]&0x80 != 0 {
		bm += strings.ToUpper(fmt.Sprintf("%016x", bytesToUint64(bitmap[8:])))
	}
	return []byte(mti + bm + body.String()), nil
}

func bytesToUint64(b []byte) uint64 {
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
	var bm2 uint64
	last := 64
	if bm&(1<<63) != 0 {
		if len(s) < 36 {
			return "", nil, fmt.Errorf("iso8583: truncated secondary bitmap")
		}
		if bm2, err = strconv.ParseUint(s[20:36], 16, 64); err != nil {
			return "", nil, fmt.Errorf("iso8583: bad secondary bitmap: %w", err)
		}
		pos, last = 36, 128
	}
	for bit := 2; bit <= last; bit++ {
		present := bit <= 64 && bm&(1<<uint(64-bit)) != 0 || bit > 64 && bm2&(1<<uint(128-bit)) != 0
		if !present {
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

// Request is the normalized input a scenario fills to produce a request message. MTI selects it:
// 0100 (authorisation, the default), 0200 (financial), 0400 (reversal) or 0800 (network management).
type Request struct {
	MTI            string
	PAN            string
	ProcessingCode string
	Amount         string // major units decimal, e.g. "150.00"
	Currency       string
	STAN           string
	Terminal       string
	Expiry         string // YYMM
	OriginalSTAN   string // the STAN of the transaction a reversal reverses
	NetworkCode    string // DE70 for 0800: 001 sign-on, 002 sign-off, 301 echo test
	POSEntryMode   string // DE22: how the card was read (051/071 chip, 010 manual, 901 contactless mag)
	PINBlock       string // DE52: ISO 9564 PIN block, 16 hex
	ICCData        string // DE55: EMV / chip data, BER-TLV as hex
	MAC            string // DE64: message authentication code, 16 or 32 hex
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
		MTI:            get("Message", "0100"),
		Expiry:         get("Expiry", ""),
		OriginalSTAN:   get("OriginalSTAN", ""),
		NetworkCode:    get("NetworkCode", "301"),
		PAN:            get("PAN", "4111111111111111"),
		ProcessingCode: get("ProcessingCode", "000000"),
		Amount:         get("Amount", "0.00"),
		Currency:       get("Currency", "EUR"),
		STAN:           get("STAN", "000001"),
		Terminal:       get("Terminal", "TERM0001"),
		POSEntryMode:   get("POSEntryMode", ""),
		PINBlock:       get("PINBlock", ""),
		ICCData:        get("ICCData", ""),
		MAC:            get("MAC", ""),
	}
}

// Encode renders the request message its MTI names.
func Encode(req Request) ([]byte, error) {
	mti := req.MTI
	if mti == "" {
		mti = "0100"
	}
	if mti == "0800" {
		return Pack(mti, map[int]string{7: transmissionTime(), 11: req.STAN, 70: req.NetworkCode})
	}
	ccy := currencyNum[strings.ToUpper(req.Currency)]
	if ccy == "" {
		ccy = "978"
	}
	f := map[int]string{
		2:  req.PAN,
		3:  req.ProcessingCode,
		4:  fmt.Sprintf("%012d", minorUnits(req.Amount)),
		7:  transmissionTime(),
		11: req.STAN,
		41: req.Terminal,
		49: ccy,
	}
	if req.Expiry != "" {
		f[14] = req.Expiry
	}
	if req.POSEntryMode != "" {
		f[22] = req.POSEntryMode
	}
	if req.PINBlock != "" {
		f[52] = req.PINBlock
	}
	if req.ICCData != "" {
		f[55] = req.ICCData
	}
	if req.MAC != "" {
		f[64] = req.MAC
	}
	switch mti {
	case "0100", "0200":
	case "0400":
		// DE90: original MTI (4), original STAN (6), original transmission date/time (10), acquiring and
		// forwarding institution ids (11 each).
		orig := req.OriginalSTAN
		if len(orig) < 6 {
			orig = strings.Repeat("0", 6-len(orig)) + orig
		}
		f[90] = "0200" + orig[:6] + strings.Repeat("0", 32)
	default:
		return nil, fmt.Errorf("iso8583: no encoder for MTI %s", mti)
	}
	return Pack(mti, f)
}

func transmissionTime() string { return time.Now().UTC().Format("0102150405") }

// Status is the normalized response: the response code and whether it approved.
type Status struct {
	MTI          string
	STAN         string
	ResponseCode string // DE39: 00 approved; 51 insufficient funds; etc.
}

// ResponseMTI is the response to a request MTI: 0100 -> 0110, 0200 -> 0210, 0400 -> 0410, 0800 -> 0810.
func ResponseMTI(mti string) string {
	if len(mti) != 4 {
		return "0110"
	}
	return mti[:2] + string(mti[2]+1) + mti[3:]
}

// EncodeResponse renders the response to a request (used by the simulated switch).
func EncodeResponse(mti string, reqFields map[int]string, de39 string) ([]byte, error) {
	if mti == "0800" {
		return Pack("0810", map[int]string{
			7: getDE(reqFields, 7, transmissionTime()), 11: getDE(reqFields, 11, "000001"), 39: de39, 70: getDE(reqFields, 70, "301"),
		})
	}
	f := map[int]string{
		3:  getDE(reqFields, 3, "000000"),
		4:  getDE(reqFields, 4, "000000000000"),
		11: getDE(reqFields, 11, "000001"),
		39: de39,
		49: getDE(reqFields, 49, "978"),
	}
	if de39 == "00" && mti != "0400" {
		f[38] = "A" + getDE(reqFields, 11, "000001")[1:] // authorisation code
	}
	return Pack(ResponseMTI(mti), f)
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
