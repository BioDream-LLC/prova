// Package mt transforms between legacy SWIFT MT messages and ISO 20022 MX messages — the migration
// every bank faces for the November 2026 cutover. It converts an MT103 (single customer credit
// transfer) to a pacs.008 Request and back, mapping the fields that carry the payment: references,
// the value-date/currency/amount triplet, the ordering and beneficiary parties, and the agents.
//
// It is deliberately a focused, readable subset — enough to drive and prove cross-format scenarios,
// not a full SWIFT parser — and it is honest about what it carries (see Transform notes in the
// manual). The point is that prova can take an MT103, transform it to MX, drive it, and prove the
// host behaved correctly on the migrated message.
package mt

import (
	"fmt"
	"strings"
	"time"

	"github.com/biodream-llc/prova/internal/iso20022"
)

// field tags this transformer understands within MT block 4.
const (
	tagRef         = "20"  // sender's reference
	tagAmount      = "32A" // value date (YYMMDD) + currency + amount
	tagOrderingAg  = "52A" // ordering institution (BIC)
	tagOrdering    = "50K" // ordering customer (account + name)
	tagAccountWith = "57A" // account-with institution (BIC)
	tagBeneficiary = "59"  // beneficiary customer (account + name)
)

// parseBlock4 extracts the field map from an MT message's text block. Values may span lines; a new
// field begins at a line matching ":tag:". Returns tag -> value (lines joined by \n).
func parseBlock4(msg string) (map[string]string, error) {
	start := strings.Index(msg, "{4:")
	block := msg
	if start >= 0 {
		block = msg[start+3:]
		if end := strings.Index(block, "-}"); end >= 0 {
			block = block[:end]
		}
	}
	fields := map[string]string{}
	var tag string
	var val []string
	flush := func() {
		if tag != "" {
			fields[tag] = strings.TrimRight(strings.Join(val, "\n"), "\n")
		}
	}
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, ":") {
			if i := strings.Index(line[1:], ":"); i >= 0 {
				flush()
				tag = line[1 : 1+i]
				val = []string{line[2+i:]}
				continue
			}
		}
		if tag != "" {
			val = append(val, line)
		}
	}
	flush()
	if len(fields) == 0 {
		return nil, fmt.Errorf("mt: no fields found (is this an MT message with a {4:...} block?)")
	}
	return fields, nil
}

// firstAccountAndName splits an MT party field (":50K:/account\nNAME") into account and name.
func firstAccountAndName(v string) (account, name string) {
	lines := strings.Split(v, "\n")
	if len(lines) > 0 {
		account = strings.TrimPrefix(strings.TrimSpace(lines[0]), "/")
	}
	if len(lines) > 1 {
		name = strings.TrimSpace(lines[1])
	}
	return
}

// parse32A decodes ":32A:YYMMDDCCYamount" into an ISO date, currency and dot-decimal amount.
func parse32A(v string) (date, ccy, amount string, err error) {
	v = strings.TrimSpace(v)
	if len(v) < 10 {
		return "", "", "", fmt.Errorf("mt: :32A: too short: %q", v)
	}
	yy, mm, dd := v[0:2], v[2:4], v[4:6]
	ccy = v[6:9]
	amount = strings.ReplaceAll(v[9:], ",", ".") // SWIFT uses a comma decimal separator
	if !strings.Contains(amount, ".") {
		amount += ".00"
	}
	date = "20" + yy + "-" + mm + "-" + dd
	return date, ccy, amount, nil
}

// MT103ToPacs008 transforms an MT103 message into a pacs.008 Request.
func MT103ToPacs008(msg string) (iso20022.Request, error) {
	f, err := parseBlock4(msg)
	if err != nil {
		return iso20022.Request{}, err
	}
	date, ccy, amount, err := parse32A(f[tagAmount])
	if err != nil {
		return iso20022.Request{}, err
	}
	dbtrAcct, _ := firstAccountAndName(f[tagOrdering])
	cdtrAcct, _ := firstAccountAndName(f[tagBeneficiary])
	ref := strings.TrimSpace(f[tagRef])
	if ref == "" {
		ref = "MT-" + time.Now().UTC().Format("150405")
	}
	return iso20022.Request{
		MsgId:      ref,
		EndToEndId: ref,
		Amount:     amount,
		Currency:   ccy,
		ValueDate:  date,
		DebtorAcct: dbtrAcct,
		CreditAcct: cdtrAcct,
		DebtorBIC:  strings.TrimSpace(f[tagOrderingAg]),
		CreditBIC:  strings.TrimSpace(f[tagAccountWith]),
	}, nil
}

// Pacs008ToMT103 transforms a pacs.008 Request into an MT103 message.
func Pacs008ToMT103(req iso20022.Request) (string, error) {
	date := strings.ReplaceAll(req.ValueDate, "-", "")
	if len(date) == 8 {
		date = date[2:] // YYYYMMDD -> YYMMDD
	}
	if date == "" {
		date = time.Now().UTC().Format("060102")
	}
	amount := strings.ReplaceAll(req.Amount, ".", ",") // dot -> SWIFT comma decimal
	var b strings.Builder
	b.WriteString("{1:F01" + bic11(req.DebtorBIC) + "0000000000}")
	b.WriteString("{2:I103" + bic11(req.CreditBIC) + "N}")
	b.WriteString("{4:\n")
	fmt.Fprintf(&b, ":20:%s\n", req.EndToEndId)
	b.WriteString(":23B:CRED\n")
	fmt.Fprintf(&b, ":32A:%s%s%s\n", date, req.Currency, amount)
	if req.DebtorBIC != "" {
		fmt.Fprintf(&b, ":52A:%s\n", req.DebtorBIC)
	}
	fmt.Fprintf(&b, ":50K:/%s\nORDERING CUSTOMER\n", req.DebtorAcct)
	if req.CreditBIC != "" {
		fmt.Fprintf(&b, ":57A:%s\n", req.CreditBIC)
	}
	fmt.Fprintf(&b, ":59:/%s\nBENEFICIARY CUSTOMER\n", req.CreditAcct)
	b.WriteString(":71A:SHA\n")
	b.WriteString("-}")
	return b.String(), nil
}

// bic11 pads an 8-char BIC to 11 with "XXX" (branch), as SWIFT headers require 11.
func bic11(b string) string {
	b = strings.TrimSpace(b)
	if b == "" {
		b = "BANKXXXX"
	}
	if len(b) == 8 {
		return b + "XXX"
	}
	return b
}
