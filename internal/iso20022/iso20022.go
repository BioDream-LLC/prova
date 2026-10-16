// Package iso20022 implements prova's first protocol adapter: ISO 20022 payment messages, the
// beachhead for the 2026 MX cutover. It encodes a pacs.008.001.08 (FI-to-FI customer credit
// transfer) from a data record, decodes the pacs.002.001.10 (payment status report) the host
// returns, and drives them over a length-prefixed TCP session. A built-in simulated host (see
// sim.go) lets scenarios run end-to-end with no external system.
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

// --- pacs.008.001.08 (request) --------------------------------------------------------------

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

type txInf struct {
	PmtId    pmtId  `xml:"PmtId"`
	Amt      amt    `xml:"IntrBkSttlmAmt"`
	SttlmDt  string `xml:"IntrBkSttlmDt,omitempty"`
	DbtrAcct acct   `xml:"DbtrAcct"`
	DbtrAgt  agent  `xml:"DbtrAgt"`
	CdtrAgt  agent  `xml:"CdtrAgt"`
	CdtrAcct acct   `xml:"CdtrAcct"`
}

type pmtId struct {
	InstrId    string `xml:"InstrId"`
	EndToEndId string `xml:"EndToEndId"`
	TxId       string `xml:"TxId"`
}

type amt struct {
	Ccy string `xml:"Ccy,attr"`
	Val string `xml:",chardata"`
}

type acct struct {
	Othr string `xml:"Id>Othr>Id"`
}

type agent struct {
	BICFI string `xml:"FinInstnId>BICFI"`
}

// Request is the normalized input a scenario fills to produce a pacs.008.
type Request struct {
	MsgId      string
	EndToEndId string
	Amount     string // decimal string, e.g. "150.00"
	Currency   string // ISO 4217, e.g. "EUR"
	ValueDate  string // interbank settlement date, ISO YYYY-MM-DD (optional)
	DebtorAcct string
	CreditAcct string
	DebtorBIC  string
	CreditBIC  string
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
		MsgId:      get("MsgId", "MSG-"+time.Now().UTC().Format("150405.000")),
		EndToEndId: get("EndToEndId", "E2E-1"),
		Amount:     get("Amount", "0.00"),
		Currency:   get("Currency", "EUR"),
		ValueDate:  get("ValueDate", ""),
		DebtorAcct: get("DebtorAcct", "ACC-DBTR-1"),
		CreditAcct: get("CreditAcct", "ACC-CDTR-1"),
		DebtorBIC:  get("DebtorBIC", "BANKDEFFXXX"),
		CreditBIC:  get("CreditBIC", "BANKFRPPXXX"),
	}
}

// Encode renders a pacs.008.001.08 document for the request.
func Encode(req Request) ([]byte, error) {
	d := doc008{
		NS: nsPacs008,
		Body: cdtTrf{
			GrpHdr: grpHdr008{
				MsgId:    req.MsgId,
				CreDtTm:  time.Now().UTC().Format(time.RFC3339),
				NbOfTxs:  "1",
				SttlmInf: sttlm{Mtd: "CLRG"},
			},
			Tx: txInf{
				PmtId:    pmtId{InstrId: req.MsgId, EndToEndId: req.EndToEndId, TxId: req.EndToEndId},
				Amt:      amt{Ccy: req.Currency, Val: req.Amount},
				SttlmDt:  req.ValueDate,
				DbtrAcct: acct{Othr: req.DebtorAcct},
				DbtrAgt:  agent{BICFI: req.DebtorBIC},
				CdtrAgt:  agent{BICFI: req.CreditBIC},
				CdtrAcct: acct{Othr: req.CreditAcct},
			},
		},
	}
	body, err := xml.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("iso20022: encode pacs.008: %w", err)
	}
	return append([]byte(xml.Header), body...), nil
}

// ParseRequest decodes a pacs.008 back into a Request (used by the simulated host).
func ParseRequest(raw []byte) (Request, error) {
	var d doc008
	if err := xml.Unmarshal(raw, &d); err != nil {
		return Request{}, fmt.Errorf("iso20022: parse pacs.008: %w", err)
	}
	t := d.Body.Tx
	return Request{
		MsgId:      d.Body.GrpHdr.MsgId,
		EndToEndId: t.PmtId.EndToEndId,
		Amount:     strings.TrimSpace(t.Amt.Val),
		Currency:   t.Amt.Ccy,
		ValueDate:  strings.TrimSpace(t.SttlmDt),
		DebtorAcct: t.DbtrAcct.Othr,
		CreditAcct: t.CdtrAcct.Othr,
		DebtorBIC:  t.DbtrAgt.BICFI,
		CreditBIC:  t.CdtrAgt.BICFI,
	}, nil
}

// --- pacs.002.001.10 (response) -------------------------------------------------------------

type doc002 struct {
	XMLName xml.Name `xml:"Document"`
	NS      string   `xml:"xmlns,attr"`
	Body    stsRpt   `xml:"FIToFIPmtStsRpt"`
}

type stsRpt struct {
	GrpHdr grpHdr002 `xml:"GrpHdr"`
	TxSts  txSts     `xml:"TxInfAndSts"`
}

type grpHdr002 struct {
	MsgId   string `xml:"MsgId"`
	CreDtTm string `xml:"CreDtTm"`
}

type txSts struct {
	OrgnlEndToEndId string  `xml:"OrgnlEndToEndId"`
	Sts             string  `xml:"TxSts"`
	Rsn             *rsnInf `xml:"StsRsnInf,omitempty"`
}

type rsnInf struct {
	Code string `xml:"Rsn>Cd"`
}

// Status is the normalized response a scenario and the casebook read.
type Status struct {
	MsgId           string
	OrgnlEndToEndId string
	TxSts           string // ACSP (accepted) | RJCT (rejected)
	ReasonCode      string // e.g. AM04 insufficient funds; empty when accepted
}

// EncodeStatus renders a pacs.002.001.10 (used by the simulated host).
func EncodeStatus(s Status) ([]byte, error) {
	b := stsRpt{
		GrpHdr: grpHdr002{MsgId: s.MsgId, CreDtTm: time.Now().UTC().Format(time.RFC3339)},
		TxSts:  txSts{OrgnlEndToEndId: s.OrgnlEndToEndId, Sts: s.TxSts},
	}
	if s.ReasonCode != "" {
		b.TxSts.Rsn = &rsnInf{Code: s.ReasonCode}
	}
	d := doc002{NS: nsPacs002, Body: b}
	body, err := xml.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("iso20022: encode pacs.002: %w", err)
	}
	return append([]byte(xml.Header), body...), nil
}

// DecodeStatus decodes a pacs.002 response.
func DecodeStatus(raw []byte) (Status, error) {
	var d doc002
	if err := xml.Unmarshal(raw, &d); err != nil {
		return Status{}, fmt.Errorf("iso20022: parse pacs.002: %w", err)
	}
	s := Status{
		MsgId:           d.Body.GrpHdr.MsgId,
		OrgnlEndToEndId: d.Body.TxSts.OrgnlEndToEndId,
		TxSts:           d.Body.TxSts.Sts,
	}
	if d.Body.TxSts.Rsn != nil {
		s.ReasonCode = d.Body.TxSts.Rsn.Code
	}
	return s, nil
}

// Fields flattens a Status into the normalized, assertable form every verdict backend understands,
// including the canonical decision keys shared across protocols.
func (s Status) Fields() map[string]string {
	f := map[string]string{
		"MsgId":           s.MsgId,
		"OrgnlEndToEndId": s.OrgnlEndToEndId,
		"TxSts":           s.TxSts,
	}
	if s.TxSts == "ACSP" {
		f["decision"] = "accepted"
	} else {
		f["decision"] = "rejected"
	}
	if s.ReasonCode != "" {
		f["RjctRsn"] = s.ReasonCode
		f["rejectReason"] = s.ReasonCode
	}
	return f
}
