// Package hsm is prova's client for HSMs that speak the Thales payShield host command protocol: a
// two-byte length, an optional message header, a two-character command code and fixed fields, over
// TCP. It implements the commands a test harness needs to check an HSM and to keep keys inside it:
//
//	NC  diagnostics (LMK check value, firmware)
//	CA  translate a PIN block from a terminal PIN key to a zone PIN key
//	EC  verify a PIN under a zone PIN key against a Visa PVV
//	CY  verify a Visa CVV / Mastercard CVC
//
// Keys are passed as the HSM holds them: encrypted under its local master key (LMK), in variant form
// with the "U" (double length) or "T" (triple length) scheme prefix. prova never sees them in clear.
//
// The wire layouts follow the payShield 9000/10K host command reference. They are tested against the
// open-source pythales simulator, which implements the same layouts (see hsm_test.go).
package hsm

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// Client talks to one HSM.
type Client struct {
	Addr    string        // host:port, e.g. 10.0.0.5:1500
	Header  string        // the message header the HSM is configured with (often 4 characters); may be empty
	Timeout time.Duration // per command; 5 s when zero
}

// Error is an HSM error code other than 00.
type Error struct {
	Command, Code string
}

func (e *Error) Error() string {
	return fmt.Sprintf("hsm: %s answered error %s (%s)", e.Command, e.Code, errorText(e.Code))
}

// errorText names the common payShield error codes.
func errorText(code string) string {
	switch code {
	case "01":
		return "verification failure"
	case "10":
		return "source key parity error"
	case "11":
		return "destination key parity error"
	case "12":
		return "no keys loaded in user storage"
	case "13":
		return "LMK error"
	case "15":
		return "invalid input data"
	case "20":
		return "PIN block does not contain valid values"
	case "23":
		return "invalid PIN block format code"
	case "24":
		return "PIN is fewer than 4 or more than 12 digits"
	case "27":
		return "key not double length"
	case "68":
		return "command disabled"
	}
	return "see the host command reference"
}

// do sends one command and returns the response fields after the response and error codes.
func (c *Client) do(ctx context.Context, command, body string) (string, error) {
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", c.Addr)
	if err != nil {
		return "", fmt.Errorf("hsm: connect %s: %w", c.Addr, err)
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	msg := c.Header + command + body
	if len(msg) > 0xFFFF {
		return "", errors.New("hsm: command too long")
	}
	frame := make([]byte, 2+len(msg))
	binary.BigEndian.PutUint16(frame, uint16(len(msg)))
	copy(frame[2:], msg)
	if _, err := conn.Write(frame); err != nil {
		return "", fmt.Errorf("hsm: send %s: %w", command, err)
	}
	var hdr [2]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		return "", fmt.Errorf("hsm: read %s reply: %w", command, err)
	}
	buf := make([]byte, binary.BigEndian.Uint16(hdr[:]))
	if _, err := io.ReadFull(conn, buf); err != nil {
		return "", fmt.Errorf("hsm: read %s reply: %w", command, err)
	}
	r := string(buf)
	if !strings.HasPrefix(r, c.Header) || len(r) < len(c.Header)+4 {
		return "", fmt.Errorf("hsm: %s reply is malformed", command)
	}
	r = r[len(c.Header):]
	want := responseCode(command)
	if r[:2] != want {
		return "", fmt.Errorf("hsm: %s answered %s, want %s", command, r[:2], want)
	}
	if code := r[2:4]; code != "00" {
		return r[4:], &Error{Command: command, Code: code}
	}
	return r[4:], nil
}

// responseCode is the command code with its second letter advanced: NC -> ND, CA -> CB.
func responseCode(cmd string) string { return cmd[:1] + string(cmd[1]+1) }

// Diagnostics is NC: the LMK check value and the firmware version.
func (c *Client) Diagnostics(ctx context.Context) (lmkCheck, firmware string, err error) {
	r, err := c.do(ctx, "NC", "")
	if err != nil {
		return "", "", err
	}
	if len(r) < 16 {
		return "", "", fmt.Errorf("hsm: NC reply too short")
	}
	return r[:16], strings.TrimSpace(r[16:]), nil
}

// FormatCode is the payShield code for an ISO 9564 PIN block format.
func FormatCode(isoFormat int) (string, error) {
	switch isoFormat {
	case 0:
		return "01", nil
	case 1:
		return "05", nil
	case 3:
		return "47", nil
	case 4:
		return "48", nil
	}
	return "", fmt.Errorf("hsm: no payShield code for PIN block format %d", isoFormat)
}

// AccountNumber is the 12 rightmost PAN digits excluding the check digit, the form PIN commands take.
func AccountNumber(pan string) (string, error) {
	if len(pan) < 13 {
		return "", fmt.Errorf("hsm: PAN too short")
	}
	return pan[len(pan)-13 : len(pan)-1], nil
}

// checkKey accepts an LMK-encrypted key: 32 hex digits, optionally with the U scheme prefix, or T and
// 48 hex digits.
func checkKey(name, k string) error {
	ok := len(k) == 32 || len(k) == 33 && k[0] == 'U' || len(k) == 49 && k[0] == 'T'
	if !ok {
		return fmt.Errorf("hsm: %s must be an LMK-encrypted key (32 hex digits, U+32 or T+48)", name)
	}
	return nil
}

// TranslatePIN is CA: decrypt a PIN block under the terminal PIN key and re-encrypt it under the zone
// PIN key, changing format if asked. It returns the PIN length and the new block.
func (c *Client) TranslatePIN(ctx context.Context, tpk, zpk string, maxPIN int, block string, from, to int, pan string) (int, string, error) {
	if err := checkKey("TPK", tpk); err != nil {
		return 0, "", err
	}
	if err := checkKey("ZPK", zpk); err != nil {
		return 0, "", err
	}
	src, err := FormatCode(from)
	if err != nil {
		return 0, "", err
	}
	dst, err := FormatCode(to)
	if err != nil {
		return 0, "", err
	}
	acct, err := AccountNumber(pan)
	if err != nil {
		return 0, "", err
	}
	if len(block) != 16 {
		return 0, "", fmt.Errorf("hsm: PIN block must be 16 hex digits")
	}
	r, err := c.do(ctx, "CA", tpk+zpk+fmt.Sprintf("%02d", maxPIN)+strings.ToUpper(block)+src+dst+acct)
	if err != nil {
		return 0, "", err
	}
	if len(r) < 18 {
		return 0, "", fmt.Errorf("hsm: CA reply too short")
	}
	n, _ := strconv.Atoi(r[:2])
	return n, r[2:18], nil
}

// VerifyPINWithPVV is EC: verify the PIN in a block under the zone PIN key against the card's Visa
// PVV, computed under the PVK pair with the PVK index. It returns false (not an error) on a mismatch.
func (c *Client) VerifyPINWithPVV(ctx context.Context, zpk, pvkPair, block string, format int, pan, pvki, pvv string) (bool, error) {
	if err := checkKey("ZPK", zpk); err != nil {
		return false, err
	}
	fc, err := FormatCode(format)
	if err != nil {
		return false, err
	}
	acct, err := AccountNumber(pan)
	if err != nil {
		return false, err
	}
	_, err = c.do(ctx, "EC", zpk+pvkPair+strings.ToUpper(block)+fc+acct+pvki+pvv)
	var he *Error
	if errors.As(err, &he) && he.Code == "01" {
		return false, nil
	}
	return err == nil, err
}

// VerifyCVV is CY: verify a CVV or CVC for a PAN, expiry (YYMM) and service code under the CVK pair.
// It returns false (not an error) on a mismatch.
func (c *Client) VerifyCVV(ctx context.Context, cvk, cvv, pan, expiry, serviceCode string) (bool, error) {
	if err := checkKey("CVK", cvk); err != nil {
		return false, err
	}
	_, err := c.do(ctx, "CY", cvk+cvv+pan+";"+expiry+serviceCode)
	var he *Error
	if errors.As(err, &he) && he.Code == "01" {
		return false, nil
	}
	return err == nil, err
}
