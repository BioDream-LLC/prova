package mt

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/biodream-llc/prova/internal/iso20022"
)

// Pair is one MT ↔ MX mapping prova carries.
type Pair struct {
	MT   string `json:"mt"`
	MX   string `json:"mx"`
	Name string `json:"name"`
}

// Pairs lists the mappings, in the order the interface offers them.
var Pairs = []Pair{
	{"MT103", "pacs.008", "single customer credit transfer"},
	{"MT202", "pacs.009", "financial institution transfer"},
	{"MT103 RETN", "pacs.004", "payment return"},
	{"MT192", "camt.056", "request for cancellation"},
	{"MT196", "camt.029", "answer to a cancellation request"},
	{"MT900", "camt.054", "confirmation of debit"},
	{"MT910", "camt.054", "confirmation of credit"},
	{"MT940", "camt.053", "customer statement"},
}

// Samples returns one example MT message per pair, for the interface to offer.
func Samples() map[string]string { return mtSamples }

var mtSamples = map[string]string{
	"MT103": "{1:F01BANKDEFFAXXX0000000000}{2:I103BANKFRPPAXXXN}{4:\n:20:E2E-MT-1\n:23B:CRED\n:32A:261015EUR90,00\n:52A:BANKDEFFXXX\n" +
		":50K:/ACC-EUR-1\nJOHN DOE\n:57A:BANKFRPPXXX\n:59:/ACC-FR-9\nJANE ROE\n:70:INVOICE 2026-0042\n:71A:SHA\n-}",
	"MT202": "{1:F01BANKDEFFAXXX0000000000}{2:I202BANKFRPPAXXXN}{4:\n:20:FI-TRF-1\n:21:REL-1\n:32A:261015EUR1000000,00\n:52A:BANKDEFFXXX\n:58A:BANKFRPPXXX\n-}",
	"MT103 RETN": "{1:F01BANKFRPPAXXX0000000000}{2:I103BANKDEFFAXXXN}{4:\n:20:RTN-1\n:23B:CRED\n:32A:261016EUR90,00\n:50K:/ACC-FR-9\nJANE ROE\n" +
		":59:/ACC-EUR-1\nJOHN DOE\n:71A:SHA\n:72:/RETN/59\n/AC03/\n/MREF/E2E-ORIG-SETTLED\n-}",
	"MT192": "{1:F01BANKDEFFAXXX0000000000}{2:I192BANKFRPPAXXXN}{4:\n:20:CXL-1\n:21:E2E-ORIG-PENDING\n:11S:103\n261015\n:79:/DUPL/ DUPLICATE PAYMENT\n-}",
	"MT196": "{1:F01BANKFRPPAXXX0000000000}{2:I196BANKDEFFAXXXN}{4:\n:20:ANS-1\n:21:CXL-1\n:76:/CNCL/\n:11R:192\n261015\n-}",
	"MT900": "{1:F01BANKDEFFAXXX0000000000}{2:I900BANKDEFFAXXXN}{4:\n:20:DBT-1\n:21:E2E-1\n:25:ACC-EUR-1\n:32A:261015EUR90,00\n-}",
	"MT910": "{1:F01BANKDEFFAXXX0000000000}{2:I910BANKDEFFAXXXN}{4:\n:20:CDT-1\n:21:E2E-2\n:25:ACC-EUR-1\n:32A:261015EUR40,50\n:52A:BANKFRPPXXX\n-}",
	"MT940": "{1:F01BANKDEFFAXXX0000000000}{2:I940BANKDEFFAXXXN}{4:\n:20:STMT-1\n:25:ACC-EUR-1\n:28C:1/1\n:60F:C261019EUR52,00\n" +
		":61:2610201020C90,00NTRFE2E-1//BANKREF1\n:61:261020D0,50NCHGFEE-OCT//BANKREF2\n:62F:C261020EUR141,50\n-}",
}

// Result is a transformed message with what was lost or assumed on the way.
type Result struct {
	From   string   `json:"from"`
	To     string   `json:"to"`
	Output string   `json:"output"`
	Notes  []string `json:"notes,omitempty"`
}

// mtType reads the message type from block 2 ({2:I103... or {2:O103...}), or infers it from the
// fields when there is no header.
func mtType(msg string, f map[string]string) string {
	if m := regexp.MustCompile(`\{2:[IO](\d{3})`).FindStringSubmatch(msg); m != nil {
		t := "MT" + m[1]
		if t == "MT103" && strings.Contains(f["72"], "/RETN/") {
			return "MT103 RETN"
		}
		return t
	}
	switch {
	case f["61"] != "" || f["62F"] != "" || f["60F"] != "":
		return "MT940"
	case f["11S"] != "":
		return "MT192"
	case f["76"] != "":
		return "MT196"
	case f["58A"] != "":
		return "MT202"
	case strings.Contains(f["72"], "/RETN/"):
		return "MT103 RETN"
	}
	return "MT103"
}

// ToMX transforms an MT message into the ISO 20022 message of its pair.
func ToMX(msg string) (Result, error) {
	f, err := parseBlock4(msg)
	if err != nil {
		return Result{}, err
	}
	t := mtType(msg, f)
	res := Result{From: t}
	var notes []string
	enc := func(r iso20022.Request) error {
		b, err := iso20022.Encode(r)
		res.Output = string(b)
		return err
	}
	switch t {
	case "MT103":
		r, err := MT103ToPacs008(msg)
		if err != nil {
			return Result{}, err
		}
		res.To, res.Notes = "pacs.008", []string{"ordering and beneficiary names travel as the debtor and creditor names; :71A: becomes the charge bearer; field 70 becomes RmtInf/Ustrd when present"}
		if v := strings.TrimSpace(f["70"]); v != "" {
			r.Remittance = strings.ReplaceAll(v, "\n", " ")
		}
		return res, enc(r)
	case "MT202":
		date, ccy, amount, err := parse32A(f["32A"])
		if err != nil {
			return Result{}, err
		}
		ref := strings.TrimSpace(f["20"])
		r := iso20022.Request{Kind: iso20022.KindPacs009, MsgId: ref, EndToEndId: orName(strings.TrimSpace(f["21"]), ref),
			Amount: amount, Currency: ccy, ValueDate: date,
			DebtorBIC: orName(bicOf(f["52A"]), senderBIC(msg)), CreditBIC: bicOf(f["58A"]),
			DebtorAcct: "NOTPROVIDED", CreditAcct: orName(accountLine(f["58A"]), "NOTPROVIDED")}
		if r.CreditBIC == "" {
			return Result{}, fmt.Errorf("mt: MT202 has no beneficiary institution (:58A:)")
		}
		notes = append(notes, ":21: (related reference) becomes the EndToEndId; :57A: (account with institution) is not carried")
		res.To, res.Notes = "pacs.009", notes
		return res, enc(r)
	case "MT103 RETN":
		date, ccy, amount, err := parse32A(f["32A"])
		if err != nil {
			return Result{}, err
		}
		reason, mref := retnInfo(f["72"])
		r := iso20022.Request{Kind: iso20022.KindPacs004, MsgId: strings.TrimSpace(f["20"]), EndToEndId: strings.TrimSpace(f["20"]),
			OrgnlEndToEndId: mref, Amount: amount, Currency: ccy, ValueDate: date, Reason: reason}
		if mref == "" {
			return Result{}, fmt.Errorf("mt: the return's :72: has no /MREF/ (the original's reference)")
		}
		res.To, res.Notes = "pacs.004", []string{":72: /RETN/ reason becomes RtrRsnInf/Rsn/Cd; /MREF/ becomes OrgnlEndToEndId"}
		return res, enc(r)
	case "MT192":
		r := iso20022.Request{Kind: iso20022.KindCamt056, MsgId: strings.TrimSpace(f["20"]), EndToEndId: strings.TrimSpace(f["20"]),
			OrgnlEndToEndId: strings.TrimSpace(f["21"]), Reason: codeIn(f["79"]),
			DebtorBIC: senderBIC(msg), CreditBIC: receiverBIC(msg)}
		if r.Reason == "" {
			r.Reason = "CUST"
			notes = append(notes, "no /CODE/ in :79:; the cancellation reason defaults to CUST (requested by customer)")
		}
		res.To, res.Notes = "camt.056", append(notes, ":21: (the original's :20:) becomes OrgnlEndToEndId; :11S: is not carried")
		return res, enc(r)
	case "MT196":
		answer := codeIn(f["76"])
		st := iso20022.Status{Kind: iso20022.KindCamt029, MsgId: strings.TrimSpace(f["20"]), OrgnlEndToEndId: strings.TrimSpace(f["21"])}
		switch answer {
		case "CNCL":
			st.TxSts = "CNCL"
		default:
			st.TxSts, st.ReasonCode = "RJCR", reasonAfter(f["76"], answer)
			notes = append(notes, "answer "+orName(answer, "(none)")+" maps to a refused cancellation (RJCR)")
		}
		b, err := iso20022.EncodeStatus(st)
		res.To, res.Output, res.Notes = "camt.029", string(b), append(notes, ":21: becomes OrgnlEndToEndId")
		return res, err
	case "MT900", "MT910":
		date, ccy, amount, err := parse32A(f["32A"])
		if err != nil {
			return Result{}, err
		}
		ind := "DBIT"
		if t == "MT910" {
			ind = "CRDT"
		}
		st := iso20022.Status{Kind: iso20022.KindCamt054, MsgId: strings.TrimSpace(f["20"]), Account: strings.TrimSpace(f["25"]), Currency: ccy,
			EntryList: []iso20022.Entry{{Amount: amount, Currency: ccy, CreditDebit: ind, Ref: strings.TrimSpace(f["21"]), ValueDate: date}}}
		b, err := iso20022.EncodeStatus(st)
		res.To, res.Output, res.Notes = "camt.054", string(b), []string{":21: becomes the entry reference; :32A: value date becomes the entry's value date"}
		return res, err
	case "MT940":
		st, ns, err := mt940ToStatus(f, repeated(msg, "61"))
		if err != nil {
			return Result{}, err
		}
		b, err := iso20022.EncodeStatus(st)
		res.To, res.Output, res.Notes = "camt.053", string(b), ns
		return res, err
	}
	return Result{}, fmt.Errorf("mt: %s has no MX pair", t)
}

// ToMT transforms an ISO 20022 message into the MT message of its pair.
func ToMT(raw []byte) (Result, error) {
	kind := iso20022.Kind(raw)
	res := Result{From: kind}
	switch kind {
	case iso20022.KindPacs008:
		r, err := iso20022.ParseRequest(raw)
		if err != nil {
			return Result{}, err
		}
		out, err := Pacs008ToMT103(r)
		res.To, res.Output, res.Notes = "MT103", out, refNotes(r.EndToEndId)
		return res, err
	case iso20022.KindPacs009:
		r, err := iso20022.ParseRequest(raw)
		if err != nil {
			return Result{}, err
		}
		var b strings.Builder
		header(&b, "202", r.DebtorBIC, r.CreditBIC)
		fmt.Fprintf(&b, ":20:%s\n:21:%s\n:32A:%s%s%s\n", ref16(r.MsgId), ref16(r.EndToEndId), mtDate(r.ValueDate), r.Currency, mtAmount(r.Amount))
		if r.DebtorBIC != "" {
			fmt.Fprintf(&b, ":52A:%s\n", r.DebtorBIC)
		}
		fmt.Fprintf(&b, ":58A:%s\n-}", r.CreditBIC)
		res.To, res.Output, res.Notes = "MT202", b.String(), refNotes(r.MsgId, r.EndToEndId)
		return res, nil
	case iso20022.KindPacs004:
		r, err := iso20022.ParseRequest(raw)
		if err != nil {
			return Result{}, err
		}
		var b strings.Builder
		header(&b, "103", "BANKFRPPXXX", "BANKDEFFXXX")
		fmt.Fprintf(&b, ":20:%s\n:23B:CRED\n:32A:%s%s%s\n:50K:/NOTPROVIDED\nRETURNING CUSTOMER\n:59:/NOTPROVIDED\nORIGINAL ORDERING CUSTOMER\n:71A:SHA\n:72:/RETN/59\n/%s/\n/MREF/%s\n-}",
			ref16(r.EndToEndId), mtDate(r.ValueDate), r.Currency, mtAmount(r.Amount), orName(r.Reason, "NARR"), ref16(r.OrgnlEndToEndId))
		res.To, res.Output = "MT103 RETN", b.String()
		res.Notes = append(refNotes(r.EndToEndId, r.OrgnlEndToEndId), "the parties of the original are not in the pacs.004 prova reads; they are written as NOTPROVIDED")
		return res, nil
	case iso20022.KindCamt056:
		r, err := iso20022.ParseRequest(raw)
		if err != nil {
			return Result{}, err
		}
		var b strings.Builder
		header(&b, "192", r.DebtorBIC, r.CreditBIC)
		fmt.Fprintf(&b, ":20:%s\n:21:%s\n:11S:103\n%s\n:79:/%s/\n-}", ref16(r.EndToEndId), ref16(r.OrgnlEndToEndId), time.Now().UTC().Format("060102"), orName(r.Reason, "CUST"))
		res.To, res.Output = "MT192", b.String()
		res.Notes = append(refNotes(r.EndToEndId, r.OrgnlEndToEndId), ":11S: names the original as an MT103 of today; camt.056 does not carry its date")
		return res, nil
	case iso20022.KindCamt029:
		st, err := iso20022.DecodeStatus(raw)
		if err != nil {
			return Result{}, err
		}
		answer := "/CNCL/"
		if st.TxSts != "CNCL" {
			answer = "/RJCR/" + st.ReasonCode
		}
		var b strings.Builder
		header(&b, "196", "BANKFRPPXXX", "BANKDEFFXXX")
		fmt.Fprintf(&b, ":20:%s\n:21:%s\n:76:%s\n:11R:192\n%s\n-}", ref16(st.MsgId), ref16(st.OrgnlEndToEndId), answer, time.Now().UTC().Format("060102"))
		res.To, res.Output, res.Notes = "MT196", b.String(), refNotes(st.MsgId, st.OrgnlEndToEndId)
		return res, nil
	case iso20022.KindCamt054:
		st, err := iso20022.DecodeStatus(raw)
		if err != nil {
			return Result{}, err
		}
		if len(st.EntryList) != 1 {
			return Result{}, fmt.Errorf("mt: an MT900 or MT910 confirms one entry; this camt.054 has %d", len(st.EntryList))
		}
		e := st.EntryList[0]
		t := "910"
		if e.CreditDebit == "DBIT" {
			t = "900"
		}
		var b strings.Builder
		header(&b, t, "BANKDEFFXXX", "BANKFRPPXXX")
		fmt.Fprintf(&b, ":20:%s\n:21:%s\n:25:%s\n:32A:%s%s%s\n-}", ref16(st.MsgId), ref16(orName(e.Ref, "NONREF")), st.Account, mtDate(e.ValueDate), e.Currency, mtAmount(e.Amount))
		res.To, res.Output, res.Notes = "MT"+t, b.String(), refNotes(st.MsgId, e.Ref)
		return res, nil
	case iso20022.KindCamt053:
		st, err := iso20022.DecodeStatus(raw)
		if err != nil {
			return Result{}, err
		}
		out, ns := statusToMT940(st)
		res.To, res.Output, res.Notes = "MT940", out, ns
		return res, nil
	}
	return Result{}, fmt.Errorf("mt: %s has no MT pair (prova maps %s)", orName(kind, "this message"), mxList())
}

func mxList() string {
	var out []string
	for _, p := range Pairs {
		out = append(out, p.MX)
	}
	return strings.Join(out, ", ")
}

// --- MT940 ---------------------------------------------------------------------------------

// balanceField parses ":60F:" / ":62F:" "C261019EUR52,00" into a signed decimal, date and currency.
func balanceField(v string) (amount, date, ccy string, err error) {
	v = strings.TrimSpace(v)
	if len(v) < 11 || (v[0] != 'C' && v[0] != 'D') {
		return "", "", "", fmt.Errorf("mt: balance %q is not C|D YYMMDD CCY amount", v)
	}
	amount = strings.ReplaceAll(v[10:], ",", ".")
	if strings.HasSuffix(amount, ".") {
		amount += "00"
	}
	if !strings.Contains(amount, ".") {
		amount += ".00"
	}
	if v[0] == 'D' {
		amount = "-" + amount
	}
	return amount, "20" + v[1:3] + "-" + v[3:5] + "-" + v[5:7], v[7:10], nil
}

var re61 = regexp.MustCompile(`^(\d{6})(\d{4})?(R?[CD])([A-Z])?([\d,]+)([A-Z]\w{3})([^/\n]*)(?://([^\n]*))?`)

func mt940ToStatus(f map[string]string, lines []string) (iso20022.Status, []string, error) {
	closing, cdate, ccy, err := balanceField(f["62F"])
	if err != nil {
		return iso20022.Status{}, nil, err
	}
	st := iso20022.Status{Kind: iso20022.KindCamt053, MsgId: strings.TrimSpace(f["20"]), Account: strings.TrimSpace(f["25"]),
		Currency: ccy, ClosingBalance: closing, ClosingDate: cdate}
	if f["60F"] != "" {
		if open, odate, _, err := balanceField(f["60F"]); err == nil {
			st.OpeningBalance, st.OpeningDate = open, odate
		}
	}
	notes := []string{":60F: and :62F: become the OPBD and CLBD balances; each :61: line becomes a booked entry"}
	for _, line := range lines {
		line = strings.TrimSpace(strings.SplitN(line, "\n", 2)[0])
		if line == "" {
			continue
		}
		m := re61.FindStringSubmatch(line)
		if m == nil {
			if len(st.EntryList) > 0 { // a continuation (the supplementary details line)
				continue
			}
			return iso20022.Status{}, nil, fmt.Errorf("mt: :61: line %q is not a statement line", line)
		}
		ind := "CRDT"
		if strings.HasSuffix(m[3], "D") {
			ind = "DBIT"
		}
		amt := strings.ReplaceAll(m[5], ",", ".")
		if strings.HasSuffix(amt, ".") {
			amt += "00"
		}
		st.EntryList = append(st.EntryList, iso20022.Entry{Amount: amt, Currency: ccy, CreditDebit: ind,
			Ref: strings.TrimSpace(m[7]), ValueDate: "20" + m[1][0:2] + "-" + m[1][2:4] + "-" + m[1][4:6]})
	}
	st.Entries = len(st.EntryList)
	return st, notes, nil
}

func statusToMT940(st iso20022.Status) (string, []string) {
	var b strings.Builder
	header(&b, "940", "BANKDEFFXXX", "BANKDEFFXXX")
	bal := func(v, date string) string {
		ind := "C"
		if strings.HasPrefix(v, "-") {
			ind, v = "D", v[1:]
		}
		return ind + mtDate(date) + st.Currency + mtAmount(v)
	}
	fmt.Fprintf(&b, ":20:%s\n:25:%s\n:28C:1/1\n", ref16(st.MsgId), st.Account)
	opening := st.OpeningBalance
	notes := refNotes(st.MsgId)
	if opening == "" {
		// Work the opening balance back from the closing one and the entries.
		var sum int64
		for _, e := range st.EntryList {
			v := minor(e.Amount)
			if e.CreditDebit == "DBIT" {
				v = -v
			}
			sum += v
		}
		opening = signed(minor(st.ClosingBalance) - sum)
		notes = append(notes, "the camt.053 has no opening balance (OPBD); :60F: is the closing balance less the entries")
	}
	fmt.Fprintf(&b, ":60F:%s\n", bal(opening, orName(st.OpeningDate, st.ClosingDate)))
	for _, e := range st.EntryList {
		ind := "C"
		if e.CreditDebit == "DBIT" {
			ind = "D"
		}
		fmt.Fprintf(&b, ":61:%s%s%sNTRF%s//NONREF\n", mtDate(e.ValueDate), ind, mtAmount(e.Amount), ref16(orName(e.Ref, "NONREF")))
	}
	fmt.Fprintf(&b, ":62F:%s\n-}", bal(st.ClosingBalance, st.ClosingDate))
	return b.String(), notes
}

// --- helpers --------------------------------------------------------------------------------

func header(b *strings.Builder, mt, sender, receiver string) {
	b.WriteString("{1:F01" + bic12(sender) + "0000000000}")
	b.WriteString("{2:I" + mt + bic12(receiver) + "N}")
	b.WriteString("{4:\n")
}

// bic12 is the 12-character logical terminal address of block 1 and 2: the BIC8, the terminal code A
// and the branch.
func bic12(b string) string {
	b = bic11(b)
	return b[:8] + "A" + b[8:]
}

func senderBIC(msg string) string {
	if m := regexp.MustCompile(`\{1:F01([A-Z0-9]{12})`).FindStringSubmatch(msg); m != nil {
		return m[1][:8] + m[1][9:]
	}
	return "BANKDEFFXXX"
}

func receiverBIC(msg string) string {
	if m := regexp.MustCompile(`\{2:I\d{3}([A-Z0-9]{12})`).FindStringSubmatch(msg); m != nil {
		return m[1][:8] + m[1][9:]
	}
	return "BANKFRPPXXX"
}

// bicOf reads the BIC of an option-A party field (an optional /account line, then the BIC).
func bicOf(v string) string {
	for _, l := range strings.Split(v, "\n") {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "/") {
			return l
		}
	}
	return ""
}

func accountLine(v string) string {
	l := strings.TrimSpace(strings.SplitN(v, "\n", 2)[0])
	if strings.HasPrefix(l, "/") {
		return strings.TrimPrefix(l, "/")
	}
	return ""
}

// retnInfo reads a return's reason code and original reference from field 72:
// "/RETN/59\n/AC01/\n/MREF/E2E-1".
func retnInfo(v string) (reason, mref string) {
	for _, l := range strings.Split(v, "\n") {
		l = strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(l, "/MREF/"):
			mref = strings.TrimPrefix(l, "/MREF/")
		case strings.HasPrefix(l, "/RETN/"):
		case regexp.MustCompile(`^/[A-Z]{2}\d{2}/`).MatchString(l):
			reason = l[1:5]
		}
	}
	return
}

// codeIn reads the first /CODE/ in a narrative field.
func codeIn(v string) string {
	if m := regexp.MustCompile(`/([A-Z]{4})/`).FindStringSubmatch(v); m != nil {
		return m[1]
	}
	return ""
}

func reasonAfter(v, code string) string {
	i := strings.Index(v, "/"+code+"/")
	if i < 0 {
		return ""
	}
	rest := strings.TrimSpace(v[i+len(code)+2:])
	if m := regexp.MustCompile(`^[A-Z0-9]{4}`).FindString(rest); m != "" {
		return m
	}
	return ""
}

// ref16 fits a reference into an MT 16x field: SWIFT's character set, at most 16 characters.
func ref16(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 16 {
		s = s[:16]
	}
	if s == "" {
		return "NONREF"
	}
	return s
}

func refNotes(refs ...string) []string {
	var out []string
	for _, r := range refs {
		if len(strings.TrimSpace(r)) > 16 {
			out = append(out, "reference "+r+" is longer than the 16 characters an MT reference field allows; it was cut to "+ref16(r))
		}
	}
	return out
}

func mtDate(iso string) string {
	d := strings.ReplaceAll(iso, "-", "")
	if len(d) == 8 {
		return d[2:]
	}
	return time.Now().UTC().Format("060102")
}

func mtAmount(a string) string {
	a = strings.TrimSpace(strings.TrimPrefix(a, "-"))
	if !strings.Contains(a, ".") {
		a += "."
	}
	return strings.ReplaceAll(a, ".", ",")
}

func orName(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

func minor(s string) int64 {
	s = strings.TrimSpace(s)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimLeft(s, "+-")
	whole, frac, _ := strings.Cut(s, ".")
	frac = (frac + "00")[:2]
	var w, f int64
	fmt.Sscan(whole, &w)
	fmt.Sscan(frac, &f)
	v := w*100 + f
	if neg {
		return -v
	}
	return v
}

func signed(v int64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	s := fmt.Sprintf("%d.%02d", v/100, v%100)
	if neg {
		return "-" + s
	}
	return s
}
