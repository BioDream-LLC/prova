package iso8583

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// A Dialect is how a network puts ISO 8583 on the wire: how numbers, text, binary data, lengths, the
// MTI and the bitmaps are encoded, and what header precedes the message. The data elements and their
// meaning are the same in every dialect; prova's scenarios run unchanged against any of them.
//
//	generic     ASCII throughout: digits, text, hex bitmaps and hex PIN/MAC/chip data (ISO 8583:1987 as
//	            most test tools and many acquirer hosts speak it)
//	visa        Visa-style: BCD numbers and MTI, binary bitmaps, EBCDIC text, binary PIN block, MAC and
//	            chip data, one-byte binary lengths, and the 22-byte BASE I-style message header
//	mastercard  Mastercard-style: EBCDIC digits, text and lengths, binary bitmaps, binary PIN block, MAC
//	            and chip data
//
// The Visa and Mastercard dialects follow public descriptions of their encodings. The schemes' own
// specifications are confidential; certify on the schemes' test systems.
type Dialect struct {
	Name    string
	numeric enc // n fields and the MTI
	text    enc // an / ans fields
	lengths enc // LL and LLL prefixes
	binary  bool
	header  bool // the 22-byte Visa-style header
}

type enc int

const (
	ascii enc = iota
	bcd
	ebcdic
	bin // lengths only: a one-byte (LL) or two-byte (LLL) binary count
)

// Dialects lists the dialects prova speaks.
var Dialects = []Dialect{Generic, Visa, Mastercard}

var (
	Generic    = Dialect{Name: "generic", numeric: ascii, text: ascii, lengths: ascii}
	Visa       = Dialect{Name: "visa", numeric: bcd, text: ebcdic, lengths: bin, binary: true, header: true}
	Mastercard = Dialect{Name: "mastercard", numeric: ebcdic, text: ebcdic, lengths: ebcdic, binary: true}
)

// DialectByName returns the dialect called name ("" is generic).
func DialectByName(name string) (Dialect, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "generic", "ascii", "iso87":
		return Generic, nil
	case "visa", "base1", "vip":
		return Visa, nil
	case "mastercard", "mc", "cis":
		return Mastercard, nil
	}
	return Dialect{}, fmt.Errorf("iso8583: unknown dialect %q (generic, visa or mastercard)", name)
}

// The code page 037 (US/Canada EBCDIC) codes for printable ASCII 0x20-0x7E.
const cp037 = "405a7f7b5b6c507d4d5d5c4e6b604b61f0f1f2f3f4f5f6f7f8f97a5e4c7e6e6f7cc1c2c3c4c5c6c7c8c9d1d2d3d4d5d6d7d8d9e2e3e4e5e6e7e8e9bae0bbb06d79818283848586878889919293949596979899a2a3a4a5a6a7a8a9c04fd0a1"

var toEBCDIC, fromEBCDIC [256]byte

func init() {
	t, _ := hex.DecodeString(cp037)
	for i := range fromEBCDIC {
		fromEBCDIC[i] = '?'
	}
	for i, e := range t {
		toEBCDIC[0x20+i] = e
		fromEBCDIC[e] = byte(0x20 + i)
	}
}

func encodeText(s string, e enc) ([]byte, error) {
	if e != ebcdic {
		return []byte(s), nil
	}
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c > 0x7E {
			return nil, fmt.Errorf("iso8583: %q has no EBCDIC code", c)
		}
		out[i] = toEBCDIC[c]
	}
	return out, nil
}

func decodeText(b []byte, e enc) string {
	if e != ebcdic {
		return string(b)
	}
	out := make([]byte, len(b))
	for i, c := range b {
		out[i] = fromEBCDIC[c]
	}
	return string(out)
}

// encodeDigits encodes a digit string: ASCII or EBCDIC characters, or packed BCD (an odd count is
// padded with a leading zero nibble).
func encodeDigits(s string, e enc) ([]byte, error) {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return nil, fmt.Errorf("iso8583: %q is not numeric", s)
		}
	}
	if e == bcd {
		if len(s)%2 == 1 {
			s = "0" + s
		}
		return hex.DecodeString(s)
	}
	return encodeText(s, e)
}

// digitBytes is how many bytes n digits take in an encoding.
func digitBytes(n int, e enc) int {
	if e == bcd {
		return (n + 1) / 2
	}
	return n
}

func decodeDigits(b []byte, n int, e enc) (string, error) {
	if e == bcd {
		s := hex.EncodeToString(b)
		s = s[len(s)-n:]
		for i := 0; i < len(s); i++ {
			if s[i] < '0' || s[i] > '9' {
				return "", fmt.Errorf("iso8583: BCD %q is not numeric", s)
			}
		}
		return s, nil
	}
	return decodeText(b, e), nil
}

func (d Dialect) encodeLength(n, width int) ([]byte, error) {
	switch d.lengths {
	case bin:
		if width == 2 {
			if n > 0xFF {
				return nil, fmt.Errorf("iso8583: length %d does not fit one byte", n)
			}
			return []byte{byte(n)}, nil
		}
		b := make([]byte, 2)
		binary.BigEndian.PutUint16(b, uint16(n))
		return b, nil
	}
	return encodeDigits(fmt.Sprintf("%0*d", width, n), d.lengths)
}

func (d Dialect) lengthBytes(width int) int {
	if d.lengths == bin {
		return width - 1
	}
	return width
}

func (d Dialect) decodeLength(b []byte) (int, error) {
	if d.lengths == bin {
		n := 0
		for _, c := range b {
			n = n<<8 | int(c)
		}
		return n, nil
	}
	s, err := decodeDigits(b, len(b), d.lengths)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(s)
}

// field data types.
const (
	tNumeric = iota
	tText
	tBinary // carried in prova as hex; binary on the wire in the binary dialects
)

func typeOf(de int) int {
	switch de {
	case 2, 3, 4, 7, 11, 14, 22, 49, 70, 90:
		return tNumeric
	case 52, 55, 64:
		return tBinary
	}
	return tText
}

// Pack encodes an MTI and data elements in the dialect.
func (d Dialect) Pack(mti string, fields map[int]string) ([]byte, error) {
	if d.Name == "" || d.Name == "generic" {
		return Pack(mti, fields)
	}
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
			bitmap[0] |= 0x80
		}
	}
	out, err := encodeDigits(mti, d.numeric)
	if err != nil {
		return nil, err
	}
	if bitmap[0]&0x80 != 0 {
		out = append(out, bitmap[:]...)
	} else {
		out = append(out, bitmap[:8]...)
	}
	for _, de := range des {
		b, err := d.packField(de, fields[de])
		if err != nil {
			return nil, err
		}
		out = append(out, b...)
	}
	if d.header {
		out = append(visaHeader(len(out)), out...)
	}
	return out, nil
}

func (d Dialect) packField(de int, v string) ([]byte, error) {
	s := spec[de]
	var data []byte
	var count int // what the length prefix counts: digits, characters or bytes
	var err error
	switch typeOf(de) {
	case tNumeric:
		if s.kind == fixed {
			if len(v) > s.length {
				return nil, fmt.Errorf("iso8583: DE%d value too long", de)
			}
			v = strings.Repeat("0", s.length-len(v)) + v
		}
		data, err = encodeDigits(v, d.numeric)
		count = len(v)
	case tBinary:
		data, err = hex.DecodeString(v)
		if err != nil {
			return nil, fmt.Errorf("iso8583: DE%d is not hex: %w", de, err)
		}
		count = len(data)
		if de == 52 || de == 64 {
			if len(data) != 8 {
				return nil, fmt.Errorf("iso8583: DE%d must be 8 bytes in the %s dialect", de, d.Name)
			}
			return data, nil
		}
	default:
		if s.kind == fixed {
			if len(v) > s.length {
				return nil, fmt.Errorf("iso8583: DE%d value too long", de)
			}
			v += strings.Repeat(" ", s.length-len(v))
		}
		data, err = encodeText(v, d.text)
		count = len(v)
	}
	if err != nil {
		return nil, fmt.Errorf("iso8583: DE%d: %w", de, err)
	}
	switch s.kind {
	case llvar, lllvar:
		width := 2
		if s.kind == lllvar {
			width = 3
		}
		l, err := d.encodeLength(count, width)
		if err != nil {
			return nil, fmt.Errorf("iso8583: DE%d: %w", de, err)
		}
		return append(l, data...), nil
	}
	return data, nil
}

// Unpack decodes a message in the dialect into its MTI and data elements (binary elements as hex).
func (d Dialect) Unpack(raw []byte) (string, map[int]string, error) {
	if d.Name == "" || d.Name == "generic" {
		return Unpack(raw)
	}
	if d.header {
		if len(raw) < 22 || raw[0] != 22 {
			return "", nil, fmt.Errorf("iso8583: missing the 22-byte %s header", d.Name)
		}
		raw = raw[22:]
	}
	mb := digitBytes(4, d.numeric)
	if len(raw) < mb+8 {
		return "", nil, fmt.Errorf("iso8583: message too short")
	}
	mti, err := decodeDigits(raw[:mb], 4, d.numeric)
	if err != nil {
		return "", nil, err
	}
	pos := mb
	bm := raw[pos : pos+8]
	pos += 8
	last := 64
	var bm2 []byte
	if bm[0]&0x80 != 0 {
		if len(raw) < pos+8 {
			return "", nil, fmt.Errorf("iso8583: truncated secondary bitmap")
		}
		bm2 = raw[pos : pos+8]
		pos += 8
		last = 128
	}
	present := func(bit int) bool {
		if bit <= 64 {
			return bm[(bit-1)/8]&(1<<uint(7-((bit-1)%8))) != 0
		}
		b := bit - 64
		return bm2[(b-1)/8]&(1<<uint(7-((b-1)%8))) != 0
	}
	fields := map[int]string{}
	for bit := 2; bit <= last; bit++ {
		if !present(bit) {
			continue
		}
		s, ok := spec[bit]
		if !ok {
			return "", nil, fmt.Errorf("iso8583: DE%d present but not in spec", bit)
		}
		v, n, err := d.unpackField(bit, s, raw[pos:])
		if err != nil {
			return "", nil, err
		}
		fields[bit] = v
		pos += n
	}
	return mti, fields, nil
}

func (d Dialect) unpackField(de int, s fieldSpec, b []byte) (string, int, error) {
	count, pos := s.length, 0
	if typeOf(de) == tBinary && (de == 52 || de == 64) {
		if len(b) < 8 {
			return "", 0, fmt.Errorf("iso8583: truncated DE%d", de)
		}
		return strings.ToUpper(hex.EncodeToString(b[:8])), 8, nil
	}
	if s.kind != fixed {
		width := 2
		if s.kind == lllvar {
			width = 3
		}
		lb := d.lengthBytes(width)
		if len(b) < lb {
			return "", 0, fmt.Errorf("iso8583: truncated length of DE%d", de)
		}
		n, err := d.decodeLength(b[:lb])
		if err != nil {
			return "", 0, fmt.Errorf("iso8583: bad length of DE%d", de)
		}
		count, pos = n, lb
	}
	switch typeOf(de) {
	case tNumeric:
		nb := digitBytes(count, d.numeric)
		if len(b) < pos+nb {
			return "", 0, fmt.Errorf("iso8583: truncated DE%d", de)
		}
		v, err := decodeDigits(b[pos:pos+nb], count, d.numeric)
		return v, pos + nb, err
	case tBinary:
		if len(b) < pos+count {
			return "", 0, fmt.Errorf("iso8583: truncated DE%d", de)
		}
		return strings.ToUpper(hex.EncodeToString(b[pos : pos+count])), pos + count, nil
	}
	if len(b) < pos+count {
		return "", 0, fmt.Errorf("iso8583: truncated DE%d", de)
	}
	v := decodeText(b[pos:pos+count], d.text)
	if s.kind == fixed {
		v = strings.TrimRight(v, " ")
	}
	return v, pos + count, nil
}

// visaHeader is a 22-byte BASE I-style header: header length, header format (01), text format (02),
// total message length, destination and source station ids (BCD), round-trip and BASE I flags,
// message status flags, batch number, reserved bytes and user information, all zero but the first
// five fields.
func visaHeader(bodyLen int) []byte {
	h := make([]byte, 22)
	h[0], h[1], h[2] = 22, 0x01, 0x02
	binary.BigEndian.PutUint16(h[3:5], uint16(22+bodyLen))
	copy(h[5:8], []byte{0x00, 0x00, 0x00})  // destination station id
	copy(h[8:11], []byte{0x00, 0x00, 0x00}) // source station id
	return h
}
