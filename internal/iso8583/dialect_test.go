package iso8583

import (
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

//go:embed testdata/dialects.json
var dialectVectors []byte

// TestDialectsAgreeWithPyiso8583 packs and unpacks messages encoded by an independent codec
// (pyiso8583, see oracle.py) in the Visa-style and Mastercard-style dialects.
func TestDialectsAgreeWithPyiso8583(t *testing.T) {
	var cases []struct {
		Dialect, MTI, Hex string
		Fields            map[string]string
	}
	if err := json.Unmarshal(dialectVectors, &cases); err != nil {
		t.Fatal(err)
	}
	for i, c := range cases {
		d, err := DialectByName(c.Dialect)
		if err != nil {
			t.Fatal(err)
		}
		fields := map[int]string{}
		for k, v := range c.Fields {
			de, _ := strconv.Atoi(k)
			fields[de] = v
		}
		raw, err := d.Pack(c.MTI, fields)
		if err != nil {
			t.Errorf("%d %s: pack: %v", i, c.Dialect, err)
			continue
		}
		body := raw
		if d.header {
			body = raw[22:]
		}
		if got := strings.ToUpper(hex.EncodeToString(body)); got != c.Hex {
			t.Errorf("%d %s: packed\n%s\nwant\n%s", i, c.Dialect, got, c.Hex)
		}
		mti, back, err := d.Unpack(raw)
		if err != nil || mti != c.MTI {
			t.Errorf("%d %s: unpack: %s %v", i, c.Dialect, mti, err)
			continue
		}
		for de, v := range fields {
			if back[de] != strings.TrimRight(v, " ") && back[de] != v {
				t.Errorf("%d %s: DE%d = %q, want %q", i, c.Dialect, de, back[de], v)
			}
		}
	}
	t.Logf("%d messages agree with pyiso8583", len(cases))
}

func TestVisaHeader(t *testing.T) {
	raw, err := Visa.Pack("0100", map[int]string{2: "4111111111111111", 3: "000000", 4: "000000009000", 11: "000101", 41: "TERM0001", 49: "978"})
	if err != nil {
		t.Fatal(err)
	}
	if raw[0] != 22 || raw[1] != 1 || raw[2] != 2 || int(raw[3])<<8|int(raw[4]) != len(raw) {
		t.Errorf("header % X", raw[:22])
	}
	if raw[22] != 0x01 || raw[23] != 0x00 {
		t.Errorf("MTI is not BCD: % X", raw[22:24])
	}
	if _, _, err := Visa.Unpack(raw[1:]); err == nil {
		t.Error("a message without its header unpacked")
	}
}
