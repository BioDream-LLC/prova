package cardcrypto

import (
	_ "embed"
	"encoding/json"
	"strconv"
	"testing"
)

//go:embed testdata/vectors.json
var vectorsJSON []byte

type vectors struct {
	PIN []struct {
		Format               int
		Key, PIN, PAN, Block string
	}
	MAC []struct {
		Key, Data, Retail, CBC string
	}
	CMAC []struct {
		Key, Data, MAC string
	}
	ARQC []struct {
		Version, IMK, PAN, PSN, AC string
		Tags                       map[string]string
	}
	PVV []struct {
		PVK, PAN, PVKI, PIN, PVV string
	}
	CVV []struct {
		CVK, PAN, Expiry, Service, CVV string
	}
}

func load(t *testing.T) vectors {
	t.Helper()
	var v vectors
	if err := json.Unmarshal(vectorsJSON, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func must(t *testing.T, s string) []byte {
	t.Helper()
	b, err := Hex(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestAgreesWithOracles checks every function against vectors computed by independent
// implementations (psec, pyemv, pynblock, cryptography; see oracle.py).
func TestAgreesWithOracles(t *testing.T) {
	v := load(t)
	for i, c := range v.PIN {
		got, err := DecipherPIN(c.Format, must(t, c.Key), must(t, c.Block), c.PAN)
		if err != nil || got != c.PIN {
			t.Errorf("pin %d (format %d): got %q, %v; want %q", i, c.Format, got, err, c.PIN)
		}
		if c.Format == Format0 { // format 0 is deterministic: the encipherment must match exactly
			b, err := EncipherPINBlock(c.Format, must(t, c.Key), c.PIN, c.PAN)
			if err != nil || HexUpper(b) != c.Block {
				t.Errorf("pin %d: enciphered %s, want %s (%v)", i, HexUpper(b), c.Block, err)
			}
		}
		// Round trip through prova's own encoder for every format.
		key := must(t, c.Key)
		b, err := EncipherPINBlock(c.Format, key, c.PIN, c.PAN)
		if err != nil {
			t.Fatalf("pin %d: %v", i, err)
		}
		if back, err := DecipherPIN(c.Format, key, b, c.PAN); err != nil || back != c.PIN {
			t.Errorf("pin %d: round trip %q, %v", i, back, err)
		}
	}
	for i, c := range v.MAC {
		if got, _ := MAC(MACRetail, must(t, c.Key), must(t, c.Data)); HexUpper(got) != c.Retail {
			t.Errorf("mac %d retail: %X want %s", i, got, c.Retail)
		}
		if got, _ := MAC(MACCBC, must(t, c.Key), must(t, c.Data)); HexUpper(got) != c.CBC {
			t.Errorf("mac %d cbc: %X want %s", i, got, c.CBC)
		}
	}
	for i, c := range v.CMAC {
		if got, err := CMAC(must(t, c.Key), must(t, c.Data)); err != nil || HexUpper(got) != c.MAC {
			t.Errorf("cmac %d: %X want %s (%v)", i, got, c.MAC, err)
		}
	}
	for i, c := range v.ARQC {
		tags := map[string][]byte{}
		for k, h := range c.Tags {
			tags[k] = must(t, h)
		}
		got, err := Cryptogram(c.Version, must(t, c.IMK), c.PAN, c.PSN, tags)
		if err != nil || HexUpper(got) != c.AC {
			t.Errorf("arqc %d (%s, PAN %d digits): %X want %s (%v)", i, c.Version, len(c.PAN), got, c.AC, err)
		}
	}
	for i, c := range v.PVV {
		if got, err := PVV(must(t, c.PVK), c.PAN, c.PVKI, c.PIN); err != nil || got != c.PVV {
			t.Errorf("pvv %d: %s want %s (%v)", i, got, c.PVV, err)
		}
	}
	for i, c := range v.CVV {
		if got, err := CVV(must(t, c.CVK), c.PAN, c.Expiry, c.Service); err != nil || got != c.CVV {
			t.Errorf("cvv %d: %s want %s (%v)", i, got, c.CVV, err)
		}
	}
	t.Logf("%d PIN blocks, %d DES MACs, %d CMACs, %d cryptograms, %d PVVs, %d CVVs agree with the oracles",
		len(v.PIN), len(v.MAC), len(v.CMAC), len(v.ARQC), len(v.PVV), len(v.CVV))
}

// TestPublishedVectors pins values published with the standards and the oracle libraries' own docs.
func TestPublishedVectors(t *testing.T) {
	// NIST SP 800-38B / RFC 4493 example 2 (AES-128, 16-byte message).
	k := must(t, "2b7e151628aed2a6abf7158809cf4f3c")
	if got, _ := CMAC(k, must(t, "6bc1bee22e409f96e93d7e117393172a")); HexUpper(got) != "070A16B46B4D4144F79BDD9DD04A287C" {
		t.Errorf("RFC 4493 example 2: %X", got)
	}
	if got, _ := CMAC(k, nil); HexUpper(got) != "BB1D6929E95937287FA37D129B756746" {
		t.Errorf("RFC 4493 example 1: %X", got)
	}
	// EMV option A example (pyemv documentation).
	mk, _ := MasterKeyA(must(t, "0123456789ABCDEFFEDCBA9876543210"), "12345678901234567", "01")
	if HexUpper(mk) != "73AD54688CEF2934B0979857E3C719F1" {
		t.Errorf("option A: %X", mk)
	}
	mkb, _ := MasterKeyB(must(t, "0123456789ABCDEFFEDCBA9876543210"), "12345678901234567", "01")
	if HexUpper(mkb) != "AD406D7F6D7570916D75E5DCAB8CF737" {
		t.Errorf("option B: %X", mkb)
	}
	sk, _ := CommonSessionKey(must(t, "0123456789ABCDEFFEDCBA9876543210"), must(t, "001C000000000000"))
	if HexUpper(sk) != "E9FB384AF807B940FEDCEA613461B0C4" {
		t.Errorf("common session key: %X", sk)
	}
}

func TestChipDataRoundTrip(t *testing.T) {
	imk := must(t, "0123456789ABCDEFFEDCBA9876543210")
	for _, ver := range CryptogramVersions() {
		icc, err := BuildChipData(ChipData{Version: ver, IMK: imk, PAN: "4111111111111111", PSN: "01", Amount: 9000,
			Currency: "978", Country: "276", Date: "261015", UN: "1A2B3C4D", ATC: "0042"})
		if err != nil {
			t.Fatal(err)
		}
		ok, err := VerifyARQC(ver, imk, "4111111111111111", "", icc)
		if err != nil || !ok {
			t.Errorf("%s: built chip data does not verify: %v", ver, err)
		}
		// A different amount in 9F02 must break the cryptogram.
		objs, _ := ParseTLV(icc)
		for i := range objs {
			if objs[i].Tag == "9F02" {
				objs[i].Value = must(t, "000000009001")
			}
		}
		if ok, _ := VerifyARQC(ver, imk, "4111111111111111", "", BuildTLV(objs)); ok {
			t.Errorf("%s: tampered amount still verifies", ver)
		}
	}
}

func TestPINBlockRejectsTampering(t *testing.T) {
	key := must(t, "0123456789ABCDEFFEDCBA9876543210")
	for _, f := range []int{Format0, Format1, Format3} {
		b, err := EncipherPINBlock(f, key, "1234", "4111111111111111")
		if err != nil {
			t.Fatal(err)
		}
		if pin, err := DecipherPIN(f, key, b, "4111111111111111"); err != nil || pin != "1234" {
			t.Errorf("format %d: %q %v", f, pin, err)
		}
		if f != Format1 { // formats 0 and 3 bind the PAN
			if pin, err := DecipherPIN(f, key, b, "5555555555554444"); err == nil && pin == "1234" {
				t.Errorf("format %d decodes under another PAN", f)
			}
		}
	}
	if _, err := ParseFormat("2"); err == nil {
		t.Error("format 2 is accepted")
	}
	if n, _ := ParseFormat("ISO-4"); n != 4 {
		t.Error("ISO-4 not parsed")
	}
	_ = strconv.Itoa
}
