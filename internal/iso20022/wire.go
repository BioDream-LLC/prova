package iso20022

import (
	"crypto/rand"
	"encoding/xml"
	"fmt"
	"strings"
)

// The building blocks every message shares. They are written so encoding/xml emits only what is set:
// optional containers are pointers, because a nested path tag ("Id>Othr>Id,omitempty") still emits its
// empty parents, which the schemas refuse.

type amt struct {
	Ccy string `xml:"Ccy,attr"`
	Val string `xml:",chardata"`
}

// acct is a CashAccount: an IBAN when one is given, else a proprietary identifier.
type acct struct {
	Id acctID `xml:"Id"`
}

type acctID struct {
	IBAN string `xml:"IBAN,omitempty"`
	Othr *othr  `xml:"Othr,omitempty"`
}

type othr struct {
	Id string `xml:"Id"`
}

func account(iban, other string) acct {
	if iban != "" {
		return acct{Id: acctID{IBAN: iban}}
	}
	return acct{Id: acctID{Othr: &othr{Id: other}}}
}

func (a acct) id() string {
	if a.Id.IBAN != "" {
		return a.Id.IBAN
	}
	if a.Id.Othr != nil {
		return a.Id.Othr.Id
	}
	return ""
}

// agent is a financial institution identified by BIC, by a clearing-system member id (a US routing
// number, for FedNow), or both.
type agent struct {
	FinInstnId finInstn `xml:"FinInstnId"`
}

type finInstn struct {
	BICFI       string  `xml:"BICFI,omitempty"`
	ClrSysMmbId *clrMmb `xml:"ClrSysMmbId,omitempty"`
}

type clrMmb struct {
	ClrSysId *clrSysID `xml:"ClrSysId,omitempty"`
	MmbId    string    `xml:"MmbId"`
}

type clrSysID struct {
	Cd string `xml:"Cd"`
}

func fiAgent(bic, rtn string) agent {
	a := agent{FinInstnId: finInstn{BICFI: bic}}
	if rtn != "" {
		a.FinInstnId.ClrSysMmbId = &clrMmb{ClrSysId: &clrSysID{Cd: "USABA"}, MmbId: rtn}
	}
	return a
}

func (a agent) bic() string { return a.FinInstnId.BICFI }

func (a agent) rtn() string {
	if m := a.FinInstnId.ClrSysMmbId; m != nil {
		return m.MmbId
	}
	return ""
}

// party is a debtor or creditor: a name and a structured postal address (town and country), the form
// CBPR+ and the EPC require from November 2026.
type party struct {
	Nm      string   `xml:"Nm,omitempty"`
	PstlAdr *pstlAdr `xml:"PstlAdr,omitempty"`
}

type pstlAdr struct {
	TwnNm string `xml:"TwnNm,omitempty"`
	Ctry  string `xml:"Ctry,omitempty"`
}

// person builds a party. Its country is the one in the BIC (positions 5-6) when there is one.
func person(name, bic string) party {
	p := party{Nm: name}
	if c := bicCountry(bic); c != "" {
		p.PstlAdr = &pstlAdr{TwnNm: townFor(c), Ctry: c}
	}
	return p
}

func bicCountry(bic string) string {
	if len(bic) >= 6 {
		c := bic[4:6]
		if c[0] >= 'A' && c[0] <= 'Z' && c[1] >= 'A' && c[1] <= 'Z' {
			return c
		}
	}
	return ""
}

// townFor is a plausible town for a country, so a default address is structured and realistic.
func townFor(ctry string) string {
	switch ctry {
	case "DE":
		return "Frankfurt"
	case "FR":
		return "Paris"
	case "GB":
		return "London"
	case "US":
		return "New York"
	case "NL":
		return "Amsterdam"
	case "IT":
		return "Milan"
	case "ES":
		return "Madrid"
	}
	return "Capital"
}

type pmtTpInf struct {
	SvcLvl    *code  `xml:"SvcLvl,omitempty"`
	LclInstrm *code  `xml:"LclInstrm,omitempty"`
	SeqTp     string `xml:"SeqTp,omitempty"`
}

type code struct {
	Cd string `xml:"Cd"`
}

func paymentType(svc, local, seq string) *pmtTpInf {
	if svc == "" && local == "" && seq == "" {
		return nil
	}
	p := &pmtTpInf{SeqTp: seq}
	if svc != "" {
		p.SvcLvl = &code{Cd: svc}
	}
	if local != "" {
		p.LclInstrm = &code{Cd: local}
	}
	return p
}

type rmtInf struct {
	Ustrd []string `xml:"Ustrd"`
}

func remittance(s string) *rmtInf {
	if s == "" {
		return nil
	}
	return &rmtInf{Ustrd: []string{s}}
}

type rsnCd struct {
	Rsn *code `xml:"Rsn,omitempty"`
}

func reason(c string) *rsnCd {
	if c == "" {
		return nil
	}
	return &rsnCd{Rsn: &code{Cd: c}}
}

func (r *rsnCd) code() string {
	if r == nil || r.Rsn == nil {
		return ""
	}
	return r.Rsn.Cd
}

// newUETR returns a random RFC 4122 version 4 UUID, the form CBPR+ requires for a UETR.
func newUETR() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func marshal(d any, kind string) ([]byte, error) {
	body, err := xml.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("iso20022: encode %s: %w", kind, err)
	}
	return append([]byte(xml.Header), body...), nil
}

func unmarshal(raw []byte, d any, kind string) error {
	if err := xml.Unmarshal(raw, d); err != nil {
		return fmt.Errorf("iso20022: parse %s: %w", kind, err)
	}
	return nil
}

func trim(s string) string { return strings.TrimSpace(s) }
