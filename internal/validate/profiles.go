package validate

import (
	"math/big"
	"regexp"
	"strings"
	"time"
)

// Profile is a usage guideline prova checks above the schema.
type Profile struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Messages    string `json:"messages"` // the messages it has rules for
}

// Profiles lists the usage-guideline profiles prova applies.
func Profiles() []Profile {
	return []Profile{
		{ID: "", Name: "Schema only", Description: "The official ISO 20022 schema, nothing above it.", Messages: "every bundled message"},
		{ID: "CBPR+", Name: "CBPR+ (SWIFT cross-border)", Description: "Structured or hybrid addresses (unstructured refused from November 2026), a UUIDv4 UETR, valid LEIs and BICs.", Messages: "pacs.008, pacs.009, pain.001"},
		{ID: "SEPA", Name: "SEPA Credit Transfer (EPC)", Description: "Euro only, 0.01 to 999,999,999.99, service level SEPA, charges SLEV, IBANs, names of at most 70 characters, one unstructured remittance line of at most 140, the EPC Latin character set, identifiers that neither start nor end with / nor contain //, structured or hybrid addresses.", Messages: "pacs.008, pain.001"},
		{ID: "SEPA-Inst", Name: "SEPA Instant Credit Transfer (EPC)", Description: "Every SEPA rule, plus local instrument INST.", Messages: "pacs.008, pain.001"},
		{ID: "FedNow", Name: "FedNow Service", Description: "US dollars only, at most the $1,000,000 network limit, one transaction per message, charges SLEV, agents identified by a valid ABA routing number (USABA), debtor and creditor names, a creation time not in the future.", Messages: "pacs.008, pacs.004"},
	}
}

// profileFindings applies the named profile's rules; ok is false for an unknown profile.
func profileFindings(doc *node, kind, profile string) (string, []Finding, bool) {
	switch strings.ToUpper(strings.ReplaceAll(profile, " ", "")) {
	case "":
		return "", nil, true
	case "CBPR+", "CBPR":
		return "CBPR+", cbprFindings(doc, kind), true
	case "SEPA", "SCT":
		return "SEPA", sepaFindings(doc, kind, false), true
	case "SEPA-INST", "SCTINST", "SEPAINST":
		return "SEPA-Inst", sepaFindings(doc, kind, true), true
	case "FEDNOW":
		return "FedNow", fednowFindings(doc, kind), true
	}
	return profile, nil, false
}

// --- SEPA (EPC SCT and SCT Inst interbank and customer-to-PSP implementation guidelines) ----

// The EPC's basic Latin character set: letters, digits, space and / - ? : ( ) . , ' +.
var reLatin = regexp.MustCompile(`^[A-Za-z0-9/\-?:().,'+ ]*$`)

func sepaFindings(doc *node, kind string, instant bool) []Finding {
	if !strings.HasPrefix(kind, "pacs.008") && !strings.HasPrefix(kind, "pain.001") {
		return []Finding{{Rule: "sepa.message", Severity: Note, Path: "/Document",
			Message: "prova's SEPA rules cover pacs.008 and pain.001; " + orNone(kind) + " was checked against its schema only"}}
	}
	var out []Finding
	add := func(rule string, sev Severity, n *node, msg, fix string) {
		p := "/Document"
		if n != nil {
			p = pathOf(doc, n)
		}
		out = append(out, Finding{Rule: rule, Severity: sev, Path: p, Message: msg, Fix: fix})
	}

	// Amounts: euro, between 0.01 and 999,999,999.99.
	for _, tag := range []string{"IntrBkSttlmAmt", "InstdAmt"} {
		for _, a := range findAll(doc, tag) {
			if c := a.attrs["Ccy"]; c != "EUR" {
				add("sepa.currency", Error, a, "SEPA payments are in euro, not "+orNone(c), "set Ccy=\"EUR\"")
			}
			if v, ok := new(big.Rat).SetString(strings.TrimSpace(a.text)); ok {
				lo, _ := new(big.Rat).SetString("0.01")
				hi, _ := new(big.Rat).SetString("999999999.99")
				if v.Cmp(lo) < 0 || v.Cmp(hi) > 0 {
					add("sepa.amount", Error, a, "amount "+strings.TrimSpace(a.text)+" is outside 0.01 to 999999999.99", "")
				}
			}
		}
	}
	// Service level SEPA (and local instrument INST for instant), in the group or the transaction.
	if !hasCode(doc, "SvcLvl", "SEPA") {
		add("sepa.service-level", Error, nil, "no service level SEPA (PmtTpInf/SvcLvl/Cd)", "add <PmtTpInf><SvcLvl><Cd>SEPA</Cd></SvcLvl></PmtTpInf>")
	}
	if instant && !hasCode(doc, "LclInstrm", "INST") {
		add("sepa.inst.local-instrument", Error, nil, "no local instrument INST (PmtTpInf/LclInstrm/Cd)", "SCT Inst payments carry LclInstrm/Cd INST")
	}
	// Charges: SLEV only.
	for _, c := range findAll(doc, "ChrgBr") {
		if v := strings.TrimSpace(c.text); v != "SLEV" {
			add("sepa.charge-bearer", Error, c, "charge bearer "+v+" is not allowed", "SEPA allows only SLEV")
		}
	}
	// Accounts: IBAN only.
	for _, tag := range []string{"DbtrAcct", "CdtrAcct"} {
		for _, a := range findAll(doc, tag) {
			if id := child(a, "Id"); id == nil || child(id, "IBAN") == nil {
				add("sepa.iban", Error, a, tag+" is not identified by an IBAN", "SEPA accounts are identified by IBAN only")
			}
		}
	}
	// Debtor and creditor names: mandatory, at most 70 characters.
	for _, tag := range []string{"Dbtr", "Cdtr"} {
		for _, p := range findAll(doc, tag) {
			nm := child(p, "Nm")
			switch {
			case nm == nil:
				add("sepa.name", Error, p, tag+" has no name", "the EPC rulebook makes the "+map[string]string{"Dbtr": "originator", "Cdtr": "beneficiary"}[tag]+"'s name mandatory")
			case len([]rune(strings.TrimSpace(nm.text))) > 70:
				add("sepa.name.length", Error, nm, "name is longer than 70 characters", "shorten it to 70")
			}
		}
	}
	// Remittance: one unstructured line of at most 140 characters.
	for _, r := range findAll(doc, "RmtInf") {
		var ustrd []*node
		for _, c := range r.children {
			if c.name == "Ustrd" {
				ustrd = append(ustrd, c)
			}
		}
		if len(ustrd) > 1 {
			add("sepa.remittance.once", Error, r, "more than one unstructured remittance line", "SEPA allows one Ustrd")
		}
		for _, u := range ustrd {
			if len([]rune(u.text)) > 140 {
				add("sepa.remittance.length", Error, u, "remittance information is longer than 140 characters", "shorten it to 140")
			}
		}
	}
	// Character set in free text; identifier rules.
	for _, tag := range []string{"Nm", "Ustrd", "AdrLine", "StrtNm", "TwnNm"} {
		for _, n := range findAll(doc, tag) {
			if !reLatin.MatchString(n.text) {
				add("sepa.charset", Error, n, tag+" uses characters outside the EPC Latin set", "use a-z A-Z 0-9 space and / - ? : ( ) . , ' + (or agree an extended set bilaterally)")
			}
		}
	}
	for _, tag := range []string{"MsgId", "PmtInfId", "InstrId", "EndToEndId", "TxId"} {
		for _, n := range findAll(doc, tag) {
			v := strings.TrimSpace(n.text)
			switch {
			case !reLatin.MatchString(v):
				add("sepa.identifier.charset", Error, n, tag+" uses characters outside the EPC Latin set", "")
			case strings.HasPrefix(v, "/") || strings.HasSuffix(v, "/"):
				add("sepa.identifier.slash", Error, n, tag+" starts or ends with /", "an identifier must not start or end with /")
			case strings.Contains(v, "//"):
				add("sepa.identifier.slash", Error, n, tag+" contains //", "an identifier must not contain //")
			}
		}
	}
	// Addresses: from 15 November 2026 the EPC refuses fully unstructured addresses.
	for _, pa := range findAll(doc, "PstlAdr") {
		if child(pa, "AdrLine") != nil && child(pa, "TwnNm") == nil {
			add("sepa.address.structured", Error, pa, "postal address is unstructured", "give at least TwnNm and Ctry (hybrid addresses stay allowed after 15 November 2026)")
		}
	}
	return out
}

func hasCode(doc *node, container, code string) bool {
	for _, n := range findAll(doc, container) {
		if cd := child(n, "Cd"); cd != nil && strings.TrimSpace(cd.text) == code {
			return true
		}
	}
	return false
}

// --- FedNow -------------------------------------------------------------------------------

// fedNowLimit is the FedNow network limit for a customer credit transfer and a payment return.
const fedNowLimit = "1000000.00"

func fednowFindings(doc *node, kind string) []Finding {
	if !strings.HasPrefix(kind, "pacs.008") && !strings.HasPrefix(kind, "pacs.004") {
		return []Finding{{Rule: "fednow.message", Severity: Note, Path: "/Document",
			Message: "prova's FedNow rules cover pacs.008 and pacs.004; " + orNone(kind) + " was checked against its schema only"}}
	}
	var out []Finding
	add := func(rule string, sev Severity, n *node, msg, fix string) {
		p := "/Document"
		if n != nil {
			p = pathOf(doc, n)
		}
		out = append(out, Finding{Rule: rule, Severity: sev, Path: p, Message: msg, Fix: fix})
	}
	limit, _ := new(big.Rat).SetString(fedNowLimit)
	for _, tag := range []string{"IntrBkSttlmAmt", "RtrdIntrBkSttlmAmt"} {
		for _, a := range findAll(doc, tag) {
			if c := a.attrs["Ccy"]; c != "USD" {
				add("fednow.currency", Error, a, "FedNow settles in US dollars, not "+orNone(c), "set Ccy=\"USD\"")
			}
			if v, ok := new(big.Rat).SetString(strings.TrimSpace(a.text)); ok && v.Cmp(limit) > 0 {
				add("fednow.limit", Error, a, "amount "+strings.TrimSpace(a.text)+" exceeds the FedNow network limit of $1,000,000", "the FedNow Service rejects it; participants may set a lower limit")
			}
		}
	}
	for _, n := range findAll(doc, "NbOfTxs") {
		if strings.TrimSpace(n.text) != "1" {
			add("fednow.single", Error, n, "FedNow messages carry one transaction, not "+strings.TrimSpace(n.text), "send one transaction per message")
		}
	}
	for _, c := range findAll(doc, "ChrgBr") {
		if v := strings.TrimSpace(c.text); v != "SLEV" {
			add("fednow.charge-bearer", Error, c, "charge bearer "+v+" is not SLEV", "FedNow payments use SLEV")
		}
	}
	if strings.HasPrefix(kind, "pacs.008") {
		for _, tag := range []string{"DbtrAgt", "CdtrAgt"} {
			for _, ag := range findAll(doc, tag) {
				fi := child(ag, "FinInstnId")
				var mmb, sys *node
				if fi != nil {
					if m := child(fi, "ClrSysMmbId"); m != nil {
						mmb = child(m, "MmbId")
						if s := child(m, "ClrSysId"); s != nil {
							sys = child(s, "Cd")
						}
					}
				}
				switch {
				case mmb == nil:
					add("fednow.agent.rtn", Error, ag, tag+" is not identified by a routing number", "add FinInstnId/ClrSysMmbId with ClrSysId/Cd USABA and the nine-digit RTN in MmbId")
				case sys == nil || strings.TrimSpace(sys.text) != "USABA":
					add("fednow.agent.usaba", Error, ag, tag+" member id is not in the USABA clearing system", "set ClrSysId/Cd to USABA")
				case !abaValid(strings.TrimSpace(mmb.text)):
					add("fednow.agent.rtn.check", Error, mmb, "routing number "+strings.TrimSpace(mmb.text)+" fails the ABA check digit", "an RTN is nine digits whose 3-7-1 weighted sum is a multiple of 10")
				}
			}
		}
		for _, tag := range []string{"Dbtr", "Cdtr"} {
			for _, p := range findAll(doc, tag) {
				if child(p, "Nm") == nil {
					add("fednow.name", Error, p, tag+" has no name", "FedNow identifies the parties by name, account and routing number")
				}
			}
		}
	}
	for _, c := range findAll(doc, "CreDtTm") {
		if t, err := time.Parse(time.RFC3339, strings.TrimSpace(c.text)); err == nil && t.After(time.Now().Add(5*time.Minute)) {
			add("fednow.future", Error, c, "creation time "+strings.TrimSpace(c.text)+" is in the future", "the FedNow Service rejects future-dated messages")
		}
	}
	return out
}

// abaValid checks a nine-digit ABA routing transit number: 3·d1 + 7·d2 + d3 + 3·d4 + 7·d5 + d6 + 3·d7 +
// 7·d8 + d9 must be a multiple of 10.
func abaValid(rtn string) bool {
	if len(rtn) != 9 {
		return false
	}
	w := []int{3, 7, 1, 3, 7, 1, 3, 7, 1}
	sum := 0
	for i := 0; i < 9; i++ {
		c := rtn[i]
		if c < '0' || c > '9' {
			return false
		}
		sum += int(c-'0') * w[i]
	}
	return sum%10 == 0
}
