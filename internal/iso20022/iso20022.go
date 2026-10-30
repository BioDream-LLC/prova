// Package iso20022 implements prova's ISO 20022 protocol adapter. It encodes the payment messages a
// tester sends (pacs.008, pacs.009, pacs.003, pacs.010, pacs.004, camt.056, pacs.028, pain.001 and the
// camt.060 account reporting request), decodes the replies a host returns (pacs.002, camt.029, pain.002,
// camt.052/053/054 reports and the admi.002 message reject), and drives them over a length-prefixed TCP
// session. Every message it writes is valid against the official schema (see the validate package's
// outbound test). A built-in simulated host (sim.go) lets scenarios run end-to-end with no external
// system.
package iso20022

import (
	"encoding/xml"
	"fmt"
	"strings"
	"time"
)

const (
	nsPacs008 = "urn:iso:std:iso:20022:tech:xsd:pacs.008.001.08"
	nsPacs002 = "urn:iso:std:iso:20022:tech:xsd:pacs.002.001.10"
)

// Request is the normalized input a scenario fills to produce an ISO 20022 message. Kind selects the
// message; the zero value of every other field is a sensible default.
type Request struct {
	Kind       string
	MsgId      string
	EndToEndId string
	UETR       string // RFC 4122 v4; generated when empty
	Amount     string // decimal string, e.g. "150.00"
	Currency   string // ISO 4217, e.g. "EUR"
	ValueDate  string // interbank settlement date, ISO YYYY-MM-DD (optional)
	DebtorAcct string
	CreditAcct string
	DebtorBIC  string
	CreditBIC  string
	DebtorIBAN string
	CreditIBAN string
	// DebtorRTN and CreditRTN identify US agents by ABA routing number (FedNow, Fedwire).
	DebtorRTN    string
	CreditRTN    string
	DebtorName   string
	CreditorName string
	// ServiceLevel (e.g. SEPA) and LocalInstrument (e.g. INST, CORE) qualify the payment type.
	ServiceLevel    string
	LocalInstrument string
	ChargeBearer    string // SLEV, SHAR, DEBT, CRED; SLEV by default
	Remittance      string // unstructured remittance information
	// OrgnlEndToEndId is the original a return, cancellation or status request refers to.
	OrgnlEndToEndId string
	// Reason is the return (pacs.004) or cancellation (camt.056) reason code.
	Reason string
	// Direct debits (pacs.003): the mandate and the creditor's scheme identifier.
	MandateId        string
	MandateDate      string
	SequenceType     string // FRST, RCUR, OOFF, FNAL
	CreditorSchemeId string
	// Account reporting (camt.060): the account and the report asked for.
	Account    string
	ReportKind string // camt.052, camt.053 (default) or camt.054
	FromDate   string
	ToDate     string
}

// FromRecord builds a Request from a flat string record (the adapter.Record form).
func FromRecord(r map[string]string) Request {
	get := func(k, dflt string) string {
		if v, ok := r[k]; ok && v != "" {
			return v
		}
		return dflt
	}
	return Request{
		Kind:       get("Message", KindPacs008),
		MsgId:      get("MsgId", "MSG-"+time.Now().UTC().Format("150405.000")),
		EndToEndId: get("EndToEndId", "E2E-1"),
		UETR:       get("UETR", ""),
		Amount:     get("Amount", ""),
		Currency:   get("Currency", "EUR"),
		ValueDate:  get("ValueDate", ""),
		DebtorAcct: get("DebtorAcct", "ACC-DBTR-1"),
		CreditAcct: get("CreditAcct", "ACC-CDTR-1"),
		DebtorBIC:  get("DebtorBIC", "BANKDEFFXXX"),
		CreditBIC:  get("CreditBIC", "BANKFRPPXXX"),
		DebtorIBAN: get("DebtorIBAN", ""),
		CreditIBAN: get("CreditIBAN", ""),
		DebtorRTN:  get("DebtorRTN", ""),
		CreditRTN:  get("CreditRTN", ""),

		DebtorName:      get("DebtorName", "Debtor Ltd"),
		CreditorName:    get("CreditorName", "Creditor Ltd"),
		ServiceLevel:    get("ServiceLevel", ""),
		LocalInstrument: get("LocalInstrument", ""),
		ChargeBearer:    get("ChargeBearer", "SLEV"),
		Remittance:      get("Remittance", ""),

		OrgnlEndToEndId: get("OrgnlEndToEndId", ""),
		Reason:          get("Reason", ""),

		MandateId:        get("MandateId", ""),
		MandateDate:      get("MandateDate", "2025-01-15"),
		SequenceType:     get("SequenceType", "RCUR"),
		CreditorSchemeId: get("CreditorSchemeId", "DE98ZZZ09999999999"),

		Account:    get("Account", get("DebtorAcct", "ACC-DBTR-1")),
		ReportKind: get("ReportKind", KindCamt053),
		FromDate:   get("FromDate", ""),
		ToDate:     get("ToDate", ""),
	}
}

func (r Request) uetr() string {
	if r.UETR != "" {
		return r.UETR
	}
	return newUETR()
}

// Encode renders the request as the ISO 20022 message its Kind names.
func Encode(req Request) ([]byte, error) {
	switch req.Kind {
	case "", KindPacs008:
		return encode008(req)
	case KindPacs009:
		return encode009(req)
	case KindPacs003:
		return encode003(req)
	case KindPacs010:
		return encode010(req)
	case KindPacs004:
		return encode004(req)
	case KindCamt056:
		return encode056(req)
	case KindPacs028:
		return encode028(req)
	case KindPain001:
		return encode001(req)
	case KindCamt060:
		return encode060(req)
	}
	return nil, fmt.Errorf("iso20022: no encoder for %q", req.Kind)
}

// ParseRequest decodes any message prova sends back into a Request (used by the simulated host).
func ParseRequest(raw []byte) (Request, error) {
	switch k := Kind(raw); k {
	case KindPacs008:
		return parse008(raw)
	case KindPacs009:
		return parse009(raw)
	case KindPacs003:
		return parse003(raw)
	case KindPacs010:
		return parse010(raw)
	case KindPacs004:
		return parse004(raw)
	case KindCamt056:
		return parse056(raw)
	case KindPacs028:
		return parse028(raw)
	case KindPain001:
		return parse001(raw)
	case KindCamt060:
		return parse060(raw)
	default:
		return Request{}, fmt.Errorf("iso20022: unsupported message %q", k)
	}
}

// --- pacs.008.001.08 (FI-to-FI customer credit transfer) ------------------------------------

type doc008 struct {
	XMLName xml.Name `xml:"Document"`
	NS      string   `xml:"xmlns,attr"`
	Body    cdtTrf   `xml:"FIToFICstmrCdtTrf"`
}

type cdtTrf struct {
	GrpHdr grpHdr008 `xml:"GrpHdr"`
	Tx     txInf     `xml:"CdtTrfTxInf"`
}

type grpHdr008 struct {
	MsgId    string `xml:"MsgId"`
	CreDtTm  string `xml:"CreDtTm"`
	NbOfTxs  string `xml:"NbOfTxs"`
	SttlmInf sttlm  `xml:"SttlmInf"`
}

type sttlm struct {
	Mtd string `xml:"SttlmMtd"`
}

type pmtId struct {
	InstrId    string `xml:"InstrId,omitempty"`
	EndToEndId string `xml:"EndToEndId"`
	TxId       string `xml:"TxId,omitempty"`
	UETR       string `xml:"UETR,omitempty"`
}

type txInf struct {
	PmtId    pmtId     `xml:"PmtId"`
	PmtTpInf *pmtTpInf `xml:"PmtTpInf,omitempty"`
	Amt      amt       `xml:"IntrBkSttlmAmt"`
	SttlmDt  string    `xml:"IntrBkSttlmDt,omitempty"`
	ChrgBr   string    `xml:"ChrgBr"`
	Dbtr     party     `xml:"Dbtr"`
	DbtrAcct acct      `xml:"DbtrAcct"`
	DbtrAgt  agent     `xml:"DbtrAgt"`
	CdtrAgt  agent     `xml:"CdtrAgt"`
	Cdtr     party     `xml:"Cdtr"`
	CdtrAcct acct      `xml:"CdtrAcct"`
	RmtInf   *rmtInf   `xml:"RmtInf,omitempty"`
}

func encode008(r Request) ([]byte, error) {
	return marshal(doc008{NS: nsPacs008, Body: cdtTrf{
		GrpHdr: grpHdr008{MsgId: r.MsgId, CreDtTm: nowISO(), NbOfTxs: "1", SttlmInf: sttlm{Mtd: "CLRG"}},
		Tx: txInf{
			PmtId:    pmtId{InstrId: r.MsgId, EndToEndId: r.EndToEndId, TxId: r.EndToEndId, UETR: r.uetr()},
			PmtTpInf: paymentType(r.ServiceLevel, r.LocalInstrument, ""),
			Amt:      amt{Ccy: r.Currency, Val: r.Amount},
			SttlmDt:  r.ValueDate,
			ChrgBr:   r.ChargeBearer,
			Dbtr:     person(r.DebtorName, r.DebtorBIC),
			DbtrAcct: account(r.DebtorIBAN, r.DebtorAcct),
			DbtrAgt:  fiAgent(r.DebtorBIC, r.DebtorRTN),
			CdtrAgt:  fiAgent(r.CreditBIC, r.CreditRTN),
			Cdtr:     person(r.CreditorName, r.CreditBIC),
			CdtrAcct: account(r.CreditIBAN, r.CreditAcct),
			RmtInf:   remittance(r.Remittance),
		},
	}}, KindPacs008)
}

func parse008(raw []byte) (Request, error) {
	var d doc008
	if err := unmarshal(raw, &d, KindPacs008); err != nil {
		return Request{}, err
	}
	t := d.Body.Tx
	r := Request{
		Kind: KindPacs008, MsgId: d.Body.GrpHdr.MsgId, EndToEndId: t.PmtId.EndToEndId, UETR: t.PmtId.UETR,
		Amount: trim(t.Amt.Val), Currency: t.Amt.Ccy, ValueDate: trim(t.SttlmDt), ChargeBearer: t.ChrgBr,
		DebtorAcct: t.DbtrAcct.id(), CreditAcct: t.CdtrAcct.id(), DebtorIBAN: t.DbtrAcct.Id.IBAN, CreditIBAN: t.CdtrAcct.Id.IBAN,
		DebtorBIC: t.DbtrAgt.bic(), CreditBIC: t.CdtrAgt.bic(), DebtorRTN: t.DbtrAgt.rtn(), CreditRTN: t.CdtrAgt.rtn(),
		DebtorName: t.Dbtr.Nm, CreditorName: t.Cdtr.Nm,
	}
	if p := t.PmtTpInf; p != nil {
		if p.SvcLvl != nil {
			r.ServiceLevel = p.SvcLvl.Cd
		}
		if p.LclInstrm != nil {
			r.LocalInstrument = p.LclInstrm.Cd
		}
	}
	if t.RmtInf != nil && len(t.RmtInf.Ustrd) > 0 {
		r.Remittance = t.RmtInf.Ustrd[0]
	}
	return r, nil
}

// --- pacs.002.001.10 (FI-to-FI payment status report) ---------------------------------------

type doc002 struct {
	XMLName xml.Name `xml:"Document"`
	NS      string   `xml:"xmlns,attr"`
	Body    stsRpt   `xml:"FIToFIPmtStsRpt"`
}

type stsRpt struct {
	GrpHdr   grpHdr002   `xml:"GrpHdr"`
	OrgnlGrp orgnlGrpInf `xml:"OrgnlGrpInfAndSts"`
	TxSts    txSts       `xml:"TxInfAndSts"`
}

type grpHdr002 struct {
	MsgId   string `xml:"MsgId"`
	CreDtTm string `xml:"CreDtTm"`
}

type orgnlGrpInf struct {
	OrgnlMsgId   string `xml:"OrgnlMsgId"`
	OrgnlMsgNmId string `xml:"OrgnlMsgNmId"`
}

type txSts struct {
	OrgnlEndToEndId string `xml:"OrgnlEndToEndId,omitempty"`
	Sts             string `xml:"TxSts,omitempty"`
	Rsn             *rsnCd `xml:"StsRsnInf,omitempty"`
}

// Status is the normalized reply a scenario and the casebook read: a payment status report (pacs.002,
// pain.002), a cancellation resolution (camt.029), an account report (camt.052/053/054) or a message
// reject (admi.002).
type Status struct {
	Kind            string // pacs.002 (default) | pain.002 | camt.029 | camt.052/053/054 | admi.002
	MsgId           string
	OrgnlMsgId      string
	OrgnlMsgNmId    string
	OrgnlEndToEndId string
	TxSts           string // pacs.002: ACSP, ACSC, PDNG, RJCT ...; camt.029: CNCL (cancelled) | RJCR (refused)
	ReasonCode      string // e.g. AM04 insufficient funds; empty when accepted
	// Account reports.
	Account        string
	Currency       string
	ClosingBalance string // signed decimal: negative when the closing balance is a debit
	Entries        int
}

// EncodeStatus renders the reply (used by the simulated host).
func EncodeStatus(s Status) ([]byte, error) {
	switch s.Kind {
	case KindCamt029:
		return encode029(s)
	case KindPain002:
		return encode002pain(s)
	case KindCamt052, KindCamt053, KindCamt054:
		return encodeReport(s)
	case KindAdmi002:
		return encodeAdmi002(s)
	}
	b := stsRpt{
		GrpHdr:   grpHdr002{MsgId: s.MsgId, CreDtTm: nowISO()},
		OrgnlGrp: orgnlGrpInf{OrgnlMsgId: orDefault(s.OrgnlMsgId, s.OrgnlEndToEndId), OrgnlMsgNmId: orDefault(s.OrgnlMsgNmId, "pacs.008.001.08")},
		TxSts:    txSts{OrgnlEndToEndId: s.OrgnlEndToEndId, Sts: s.TxSts, Rsn: reason(s.ReasonCode)},
	}
	return marshal(doc002{NS: nsPacs002, Body: b}, KindPacs002)
}

// DecodeStatus decodes any reply a host returns.
func DecodeStatus(raw []byte) (Status, error) {
	switch Kind(raw) {
	case KindCamt029:
		return decode029(raw)
	case KindPain002:
		return decode002pain(raw)
	case KindCamt052, KindCamt053, KindCamt054:
		return decodeReport(raw)
	case KindAdmi002:
		return decodeAdmi002(raw)
	}
	var d doc002
	if err := unmarshal(raw, &d, KindPacs002); err != nil {
		return Status{}, err
	}
	return Status{
		Kind: KindPacs002, MsgId: d.Body.GrpHdr.MsgId, OrgnlMsgId: d.Body.OrgnlGrp.OrgnlMsgId, OrgnlMsgNmId: d.Body.OrgnlGrp.OrgnlMsgNmId,
		OrgnlEndToEndId: d.Body.TxSts.OrgnlEndToEndId, TxSts: d.Body.TxSts.Sts, ReasonCode: d.Body.TxSts.Rsn.code(),
	}, nil
}

// Fields flattens a Status into the normalized, assertable form every verdict backend understands,
// including the canonical decision keys shared across protocols.
func (s Status) Fields() map[string]string {
	f := map[string]string{"MsgId": s.MsgId}
	switch s.Kind {
	case KindCamt052, KindCamt053, KindCamt054:
		f["decision"] = "accepted"
		f["reportKind"] = s.Kind
		f["reportedAccount"] = s.Account
		f["reportedBalance"] = s.ClosingBalance
		f["Ccy"] = s.Currency
		f["entries"] = fmt.Sprint(s.Entries)
		return f
	case KindAdmi002:
		f["decision"] = "rejected"
		f["rejectReason"] = s.ReasonCode
		f["RjctgPtyRsn"] = s.ReasonCode
		return f
	}
	f["OrgnlEndToEndId"] = s.OrgnlEndToEndId
	f["TxSts"] = s.TxSts
	switch s.TxSts {
	case "RJCT", "RJCR":
		f["decision"] = "rejected"
	case "":
		// no recognized status: the structural-validity rule reports it
	default:
		f["decision"] = "accepted"
		f["reportedStatus"] = s.TxSts // what a status report says of the original
	}
	if s.Kind == KindCamt029 {
		f["Conf"] = s.TxSts
		delete(f, "TxSts")
	}
	if s.ReasonCode != "" {
		f["RjctRsn"] = s.ReasonCode
		f["rejectReason"] = s.ReasonCode
	}
	return f
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// nameOf is the full message name for a namespace short name ("pacs.008" -> "pacs.008.001.08").
func nameOf(kind string) string {
	if v, ok := versions[kind]; ok {
		return kind + "." + v
	}
	return kind
}

func nowISO() string { return time.Now().UTC().Format("2006-01-02T15:04:05Z") }

func today() string { return time.Now().UTC().Format("2006-01-02") }

func orToday(d string) string {
	if d != "" {
		return d
	}
	return today()
}

func upper(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }
