package hsm

import (
	"bufio"
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/biodream-llc/prova/internal/cardcrypto"
)

// The pythales simulator's default test LMK.
const testLMK = "DEAFBEEDEAFBEEDEAFBEEDEAFBEEDEAF"

// underLMK encrypts a clear key under the test LMK, the form the HSM's commands take.
func underLMK(t *testing.T, clear string) string {
	t.Helper()
	lmk, _ := cardcrypto.Hex(testLMK)
	k, _ := cardcrypto.Hex(clear)
	e, err := cardcrypto.EncryptTDES(lmk, k)
	if err != nil {
		t.Fatal(err)
	}
	return cardcrypto.HexUpper(e)
}

// TestAgainstPythales drives prova's client against the pythales payShield simulator. It needs Python
// with pythales, pynblock and psec installed; set PROVA_TEST_PYTHALES to that python.
func TestAgainstPythales(t *testing.T) {
	py := os.Getenv("PROVA_TEST_PYTHALES")
	if py == "" {
		t.Skip("set PROVA_TEST_PYTHALES=/path/to/python with pythales, pynblock and psec installed")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	script, _ := filepath.Abs("testdata/hsmsim.py")
	const header = "SSSS"
	cmd := exec.Command(py, "-u", script, "serve", strconv.Itoa(port), header)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	// pythales cannot survive a probe connection that sends nothing, so wait for its banner instead.
	ready := make(chan bool, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			if strings.Contains(sc.Text(), "Listening") {
				ready <- true
			}
		}
	}()
	select {
	case <-ready:
	case <-time.After(10 * time.Second):
		t.Fatal("pythales did not start")
	}
	addr := "127.0.0.1:" + strconv.Itoa(port)
	ctx := context.Background()
	c := &Client{Addr: addr, Header: header}

	lmkCV, fw, err := c.Diagnostics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	lmk, _ := cardcrypto.Hex(testLMK)
	want, _ := cardcrypto.EncryptTDES(lmk, make([]byte, 8))
	if lmkCV != cardcrypto.HexUpper(want) || fw == "" {
		t.Errorf("NC: LMK check %s (want %s), firmware %q", lmkCV, cardcrypto.HexUpper(want), fw)
	}

	// pythales checks for even parity per byte (a real HSM wants odd); DES ignores the parity bits, so
	// the keys are adjusted to satisfy the simulator without changing what they encrypt.
	tpkClear, zpkClear := evenParity("0123456789ABCDEFFEDCBA9876543210"), evenParity("89ABCDEF01234567FEDCBA9876543210")
	tpk, zpk := "U"+underLMK(t, tpkClear), "U"+underLMK(t, zpkClear)
	pan := "4111111111111111"
	tpkB, _ := cardcrypto.Hex(tpkClear)
	zpkB, _ := cardcrypto.Hex(zpkClear)
	block, _ := cardcrypto.EncipherPINBlock(cardcrypto.Format0, tpkB, "1234", pan)

	n, out, err := c.TranslatePIN(ctx, tpk, zpk, 12, cardcrypto.HexUpper(block), 0, 0, pan)
	if err != nil {
		t.Fatalf("CA: %v", err)
	}
	// pythales re-encrypts under the destination key as sent (its LMK-encrypted text) rather than the
	// key it decrypts to, so the result is checked under those bytes.
	outB, _ := cardcrypto.Hex(out)
	zpkText, _ := cardcrypto.Hex(zpk[1:])
	if pin, err := cardcrypto.DecipherPIN(cardcrypto.Format0, zpkText, outB, pan); err != nil || pin != "1234" || n != 4 {
		t.Errorf("CA: translated block decodes to %q (len %d), %v", pin, n, err)
	}

	// EC: pythales computes the PVV from the PVK as sent (its key text), so the expected PVV comes from
	// the harness. This checks prova's command layout and reply handling; cardcrypto's own PVV is
	// checked against psec.
	pvk := underLMK(t, evenParity("0123456789ABCDEFFEDCBA9876543210"))
	acct, _ := AccountNumber(pan)
	pvvOut, err := exec.Command(py, script, "pvv", acct, "1", "1234", pvk).Output()
	if err != nil {
		t.Fatal(err)
	}
	pvv := strings.TrimSpace(string(pvvOut))
	zblock, _ := cardcrypto.EncipherPINBlock(cardcrypto.Format0, zpkB, "1234", pan)
	ok, err := c.VerifyPINWithPVV(ctx, zpk, pvk, cardcrypto.HexUpper(zblock), 0, pan, "1", pvv)
	if err != nil || !ok {
		t.Errorf("EC with the right PIN: %v, %v", ok, err)
	}
	wrong, _ := cardcrypto.EncipherPINBlock(cardcrypto.Format0, zpkB, "9999", pan)
	if ok, err := c.VerifyPINWithPVV(ctx, zpk, pvk, cardcrypto.HexUpper(wrong), 0, pan, "1", pvv); err != nil || ok {
		t.Errorf("EC with the wrong PIN: %v, %v", ok, err)
	}

	// CY: pythales checks the parity of the CVK it decrypts, then applies the CVV to the key as sent.
	cvkText := underLMK(t, evenParity("0123456789ABCDEFFEDCBA9876543210"))
	cvkB, _ := cardcrypto.Hex(cvkText)
	cvv, _ := cardcrypto.CVV(cvkB, pan, "3012", "101")
	if ok, err := c.VerifyCVV(ctx, "U"+cvkText, cvv, pan, "3012", "101"); err != nil || !ok {
		t.Errorf("CY with the right CVV: %v, %v", ok, err)
	}
	if ok, err := c.VerifyCVV(ctx, "U"+cvkText, "000", pan, "3012", "101"); err != nil || ok {
		t.Errorf("CY with a wrong CVV: %v, %v", ok, err)
	}
}

func evenParity(h string) string {
	b, _ := cardcrypto.Hex(h)
	for i, v := range b {
		ones := 0
		for x := v; x != 0; x >>= 1 {
			ones += int(x & 1)
		}
		if ones%2 == 1 {
			b[i] ^= 1
		}
	}
	return cardcrypto.HexUpper(b)
}

func TestFramingAndErrors(t *testing.T) {
	if responseCode("NC") != "ND" || responseCode("CA") != "CB" || responseCode("EC") != "ED" {
		t.Error("response codes")
	}
	if _, err := FormatCode(2); err == nil {
		t.Error("format 2 has no payShield code here")
	}
	if a, _ := AccountNumber("4111111111111111"); a != "111111111111" {
		t.Errorf("account number %s", a)
	}
	if err := checkKey("ZPK", "U0123"); err == nil {
		t.Error("short key accepted")
	}
	e := &Error{Command: "EC", Code: "01"}
	if !strings.Contains(e.Error(), "verification failure") {
		t.Error(e.Error())
	}
}
