package iso20022

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
	"time"
)

// The message kinds prova speaks, by their ISO 20022 short names.
const (
	KindPacs008 = "pacs.008" // FI-to-FI customer credit transfer
	KindPacs009 = "pacs.009" // financial institution credit transfer
	KindPacs004 = "pacs.004" // payment return
	KindCamt056 = "camt.056" // FI-to-FI payment cancellation request
	KindPacs028 = "pacs.028" // FI-to-FI payment status request
	KindPacs002 = "pacs.002" // FI-to-FI payment status report (response)
	KindCamt029 = "camt.029" // resolution of investigation (response to camt.056)
	KindPain001 = "pain.001" // customer credit transfer initiation
	KindPain002 = "pain.002" // customer payment status report (response to pain.001)
)

const (
	nsPacs009 = "urn:iso:std:iso:20022:tech:xsd:pacs.009.001.08"
	nsPacs004 = "urn:iso:std:iso:20022:tech:xsd:pacs.004.001.09"
	nsCamt056 = "urn:iso:std:iso:20022:tech:xsd:camt.056.001.08"
	nsPacs028 = "urn:iso:std:iso:20022:tech:xsd:pacs.028.001.03"
	nsCamt029 = "urn:iso:std:iso:20022:tech:xsd:camt.029.001.09"
	nsPain001 = "urn:iso:std:iso:20022:tech:xsd:pain.001.001.10"
	nsPain002 = "urn:iso:std:iso:20022:tech:xsd:pain.002.001.11"
)

// rootKinds maps each message's business root element (the child of Document) to its kind.
var rootKinds = map[string]string{
	"FIToFICstmrCdtTrf": KindPacs008,
	"FICdtTrf":          KindPacs009,
	"PmtRtr":            KindPacs004,
	"FIToFIPmtCxlReq":   KindCamt056,
	"FIToFIPmtStsReq":   KindPacs028,
	"FIToFIPmtStsRpt":   KindPacs002,
	"RsltnOfInvstgtn":   KindCamt029,
	"CstmrCdtTrfInitn":  KindPain001,
	"CstmrPmtStsRpt":    KindPain002,
}

// Kind identifies an ISO 20022 document by the element directly inside Document. It returns "" for
// anything else.
func Kind(raw []byte) string {
	d := xml.NewDecoder(bytes.NewReader(raw))
	depth := 0
	for {
		tok, err := d.Token()
		if err != nil {
			return ""
		}
		if se, ok := tok.(xml.StartElement); ok {
			depth++
			if depth == 1 && se.Name.Local != "Document" {
				return ""
			}
			if depth == 2 {
				return rootKinds[se.Name.Local]
			}
		}
	}
}

func nowISO() string { return time.Now().UTC().Format(time.RFC3339) }

// --- pacs.009.001.08 ------------------------------------------------------------------------

type doc009 struct {
	XMLName xml.Name `xml:"Document"`
	NS      string   `xml:"xmlns,attr"`
	Body    fiCdtTrf `xml:"FICdtTrf"`
}

type fiCdtTrf struct {
	GrpHdr grpHdr008 `xml:"GrpHdr"`
	Tx     fiTxInf   `xml:"CdtTrfTxInf"`
}

type fiTxInf struct {
	PmtId    pmtId  `xml:"PmtId"`
	Amt      amt    `xml:"IntrBkSttlmAmt"`
	SttlmDt  string `xml:"IntrBkSttlmDt,omitempty"`
	Dbtr     agent  `xml:"Dbtr"`
	DbtrAcct acct   `xml:"DbtrAcct"`
	Cdtr     agent  `xml:"Cdtr"`
	CdtrAcct acct   `xml:"CdtrAcct"`
}

func encode009(r Request) ([]byte, error) {
	return marshal(doc009{NS: nsPacs009, Body: fiCdtTrf{
		GrpHdr: grpHdr008{MsgId: r.MsgId, CreDtTm: nowISO(), NbOfTxs: "1", SttlmInf: sttlm{Mtd: "CLRG"}},
		Tx: fiTxInf{
			PmtId:    pmtId{InstrId: r.MsgId, EndToEndId: r.EndToEndId, TxId: r.EndToEndId},
			Amt:      amt{Ccy: r.Currency, Val: r.Amount},
			SttlmDt:  r.ValueDate,
			Dbtr:     agent{BICFI: r.DebtorBIC},
			DbtrAcct: account(r.DebtorIBAN, r.DebtorAcct),
			Cdtr:     agent{BICFI: r.CreditBIC},
			CdtrAcct: account(r.CreditIBAN, r.CreditAcct),
		},
	}}, KindPacs009)
}

func parse009(raw []byte) (Request, error) {
	var d doc009
	if err := xml.Unmarshal(raw, &d); err != nil {
		return Request{}, fmt.Errorf("iso20022: parse pacs.009: %w", err)
	}
	t := d.Body.Tx
	return Request{
		Kind: KindPacs009, MsgId: d.Body.GrpHdr.MsgId, EndToEndId: t.PmtId.EndToEndId,
		Amount: strings.TrimSpace(t.Amt.Val), Currency: t.Amt.Ccy, ValueDate: strings.TrimSpace(t.SttlmDt),
		DebtorAcct: t.DbtrAcct.id(), CreditAcct: t.CdtrAcct.id(), DebtorIBAN: t.DbtrAcct.IBAN, CreditIBAN: t.CdtrAcct.IBAN,
		DebtorBIC: t.Dbtr.BICFI, CreditBIC: t.Cdtr.BICFI,
	}, nil
}

// --- pacs.004.001.09 ------------------------------------------------------------------------

type doc004 struct {
	XMLName xml.Name `xml:"Document"`
	NS      string   `xml:"xmlns,attr"`
	Body    pmtRtr   `xml:"PmtRtr"`
}

type pmtRtr struct {
	GrpHdr grpHdr008 `xml:"GrpHdr"`
	Tx     rtrTxInf  `xml:"TxInf"`
}

type rtrTxInf struct {
	RtrId           string `xml:"RtrId"`
	OrgnlEndToEndId string `xml:"OrgnlEndToEndId"`
	RtrdAmt         amt    `xml:"RtrdIntrBkSttlmAmt"`
	RtrRsn          string `xml:"RtrRsnInf>Rsn>Cd,omitempty"`
}

func encode004(r Request) ([]byte, error) {
	return marshal(doc004{NS: nsPacs004, Body: pmtRtr{
		GrpHdr: grpHdr008{MsgId: r.MsgId, CreDtTm: nowISO(), NbOfTxs: "1", SttlmInf: sttlm{Mtd: "CLRG"}},
		Tx: rtrTxInf{RtrId: r.EndToEndId, OrgnlEndToEndId: r.OrgnlEndToEndId,
			RtrdAmt: amt{Ccy: r.Currency, Val: r.Amount}, RtrRsn: r.Reason},
	}}, KindPacs004)
}

func parse004(raw []byte) (Request, error) {
	var d doc004
	if err := xml.Unmarshal(raw, &d); err != nil {
		return Request{}, fmt.Errorf("iso20022: parse pacs.004: %w", err)
	}
	t := d.Body.Tx
	return Request{
		Kind: KindPacs004, MsgId: d.Body.GrpHdr.MsgId, EndToEndId: t.RtrId, OrgnlEndToEndId: t.OrgnlEndToEndId,
		Amount: strings.TrimSpace(t.RtrdAmt.Val), Currency: t.RtrdAmt.Ccy, Reason: t.RtrRsn,
	}, nil
}

// --- camt.056.001.08 ------------------------------------------------------------------------

type doc056 struct {
	XMLName xml.Name `xml:"Document"`
	NS      string   `xml:"xmlns,attr"`
	Body    cxlReq   `xml:"FIToFIPmtCxlReq"`
}

type cxlReq struct {
	Assgnmt assignment `xml:"Assgnmt"`
	Tx      cxlTxInf   `xml:"Undrlyg>TxInf"`
}

type assignment struct {
	Id      string `xml:"Id"`
	Assgnr  agent  `xml:"Assgnr>Agt"`
	Assgne  agent  `xml:"Assgne>Agt"`
	CreDtTm string `xml:"CreDtTm"`
}

type cxlTxInf struct {
	CxlId           string `xml:"CxlId"`
	OrgnlEndToEndId string `xml:"OrgnlEndToEndId"`
	OrgnlAmt        *amt   `xml:"OrgnlIntrBkSttlmAmt,omitempty"`
	CxlRsn          string `xml:"CxlRsnInf>Rsn>Cd,omitempty"`
}

func encode056(r Request) ([]byte, error) {
	tx := cxlTxInf{CxlId: r.EndToEndId, OrgnlEndToEndId: r.OrgnlEndToEndId, CxlRsn: r.Reason}
	if r.Amount != "" {
		tx.OrgnlAmt = &amt{Ccy: r.Currency, Val: r.Amount}
	}
	return marshal(doc056{NS: nsCamt056, Body: cxlReq{
		Assgnmt: assignment{Id: r.MsgId, Assgnr: agent{BICFI: r.DebtorBIC}, Assgne: agent{BICFI: r.CreditBIC}, CreDtTm: nowISO()},
		Tx:      tx,
	}}, KindCamt056)
}

func parse056(raw []byte) (Request, error) {
	var d doc056
	if err := xml.Unmarshal(raw, &d); err != nil {
		return Request{}, fmt.Errorf("iso20022: parse camt.056: %w", err)
	}
	t := d.Body.Tx
	r := Request{
		Kind: KindCamt056, MsgId: d.Body.Assgnmt.Id, EndToEndId: t.CxlId, OrgnlEndToEndId: t.OrgnlEndToEndId,
		Reason: t.CxlRsn, DebtorBIC: d.Body.Assgnmt.Assgnr.BICFI, CreditBIC: d.Body.Assgnmt.Assgne.BICFI,
	}
	if t.OrgnlAmt != nil {
		r.Amount, r.Currency = strings.TrimSpace(t.OrgnlAmt.Val), t.OrgnlAmt.Ccy
	}
	return r, nil
}

// --- pacs.028.001.03 ------------------------------------------------------------------------

type doc028 struct {
	XMLName xml.Name `xml:"Document"`
	NS      string   `xml:"xmlns,attr"`
	Body    stsReq   `xml:"FIToFIPmtStsReq"`
}

type stsReq struct {
	GrpHdr grpHdr002 `xml:"GrpHdr"`
	Tx     stsTxInf  `xml:"TxInf"`
}

type stsTxInf struct {
	StsReqId        string `xml:"StsReqId"`
	OrgnlEndToEndId string `xml:"OrgnlEndToEndId"`
}

func encode028(r Request) ([]byte, error) {
	return marshal(doc028{NS: nsPacs028, Body: stsReq{
		GrpHdr: grpHdr002{MsgId: r.MsgId, CreDtTm: nowISO()},
		Tx:     stsTxInf{StsReqId: r.EndToEndId, OrgnlEndToEndId: r.OrgnlEndToEndId},
	}}, KindPacs028)
}

func parse028(raw []byte) (Request, error) {
	var d doc028
	if err := xml.Unmarshal(raw, &d); err != nil {
		return Request{}, fmt.Errorf("iso20022: parse pacs.028: %w", err)
	}
	return Request{Kind: KindPacs028, MsgId: d.Body.GrpHdr.MsgId, EndToEndId: d.Body.Tx.StsReqId, OrgnlEndToEndId: d.Body.Tx.OrgnlEndToEndId}, nil
}

// --- camt.029.001.09 (response to camt.056) -------------------------------------------------

type doc029 struct {
	XMLName xml.Name `xml:"Document"`
	NS      string   `xml:"xmlns,attr"`
	Body    rsltn    `xml:"RsltnOfInvstgtn"`
}

type rsltn struct {
	Assgnmt assignment `xml:"Assgnmt"`
	Conf    string     `xml:"Sts>Conf"`
	Tx      cxlTxSts   `xml:"CxlDtls>TxInfAndSts"`
}

type cxlTxSts struct {
	OrgnlEndToEndId string `xml:"OrgnlEndToEndId"`
	TxCxlSts        string `xml:"TxCxlSts"`
	Rsn             string `xml:"CxlStsRsnInf>Rsn>Cd,omitempty"`
}

func encode029(s Status) ([]byte, error) {
	return marshal(doc029{NS: nsCamt029, Body: rsltn{
		Assgnmt: assignment{Id: s.MsgId, CreDtTm: nowISO()},
		Conf:    s.TxSts,
		Tx:      cxlTxSts{OrgnlEndToEndId: s.OrgnlEndToEndId, TxCxlSts: s.TxSts, Rsn: s.ReasonCode},
	}}, KindCamt029)
}

func decode029(raw []byte) (Status, error) {
	var d doc029
	if err := xml.Unmarshal(raw, &d); err != nil {
		return Status{}, fmt.Errorf("iso20022: parse camt.029: %w", err)
	}
	return Status{Kind: KindCamt029, MsgId: d.Body.Assgnmt.Id, OrgnlEndToEndId: d.Body.Tx.OrgnlEndToEndId,
		TxSts: d.Body.Conf, ReasonCode: d.Body.Tx.Rsn}, nil
}

// --- pain.001.001.10 (customer credit transfer initiation) ----------------------------------

type doc001 struct {
	XMLName xml.Name `xml:"Document"`
	NS      string   `xml:"xmlns,attr"`
	Body    cstmrCT  `xml:"CstmrCdtTrfInitn"`
}

type cstmrCT struct {
	GrpHdr grpHdrPain `xml:"GrpHdr"`
	PmtInf pmtInf     `xml:"PmtInf"`
}

type grpHdrPain struct {
	MsgId    string `xml:"MsgId"`
	CreDtTm  string `xml:"CreDtTm"`
	NbOfTxs  string `xml:"NbOfTxs"`
	InitgPty party  `xml:"InitgPty"`
}

type party struct {
	Nm string `xml:"Nm"`
}

type pmtInf struct {
	PmtInfId  string    `xml:"PmtInfId"`
	PmtMtd    string    `xml:"PmtMtd"`
	ReqdExctn reqdDt    `xml:"ReqdExctnDt"`
	Dbtr      party     `xml:"Dbtr"`
	DbtrAcct  acct      `xml:"DbtrAcct"`
	DbtrAgt   agent     `xml:"DbtrAgt"`
	Tx        painTxInf `xml:"CdtTrfTxInf"`
}

type reqdDt struct {
	Dt string `xml:"Dt"`
}

type painTxInf struct {
	PmtId    pmtId    `xml:"PmtId"`
	Amt      instdAmt `xml:"Amt"`
	CdtrAgt  agent    `xml:"CdtrAgt"`
	Cdtr     party    `xml:"Cdtr"`
	CdtrAcct acct     `xml:"CdtrAcct"`
}

type instdAmt struct {
	InstdAmt amt `xml:"InstdAmt"`
}

func encode001(r Request) ([]byte, error) {
	return marshal(doc001{NS: nsPain001, Body: cstmrCT{
		GrpHdr: grpHdrPain{MsgId: r.MsgId, CreDtTm: nowISO(), NbOfTxs: "1", InitgPty: party{Nm: "prova"}},
		PmtInf: pmtInf{
			PmtInfId: r.MsgId, PmtMtd: "TRF", ReqdExctn: reqdDt{Dt: orToday(r.ValueDate)},
			Dbtr: party{Nm: "Debtor"}, DbtrAcct: account(r.DebtorIBAN, r.DebtorAcct), DbtrAgt: agent{BICFI: r.DebtorBIC},
			Tx: painTxInf{
				PmtId:   pmtId{InstrId: r.MsgId, EndToEndId: r.EndToEndId},
				Amt:     instdAmt{InstdAmt: amt{Ccy: r.Currency, Val: r.Amount}},
				CdtrAgt: agent{BICFI: r.CreditBIC}, Cdtr: party{Nm: "Creditor"}, CdtrAcct: account(r.CreditIBAN, r.CreditAcct),
			},
		},
	}}, KindPain001)
}

func parse001(raw []byte) (Request, error) {
	var d doc001
	if err := xml.Unmarshal(raw, &d); err != nil {
		return Request{}, fmt.Errorf("iso20022: parse pain.001: %w", err)
	}
	t := d.Body.PmtInf
	return Request{
		Kind: KindPain001, MsgId: d.Body.GrpHdr.MsgId, EndToEndId: t.Tx.PmtId.EndToEndId,
		Amount: strings.TrimSpace(t.Tx.Amt.InstdAmt.Val), Currency: t.Tx.Amt.InstdAmt.Ccy,
		DebtorAcct: t.DbtrAcct.id(), CreditAcct: t.Tx.CdtrAcct.id(), DebtorIBAN: t.DbtrAcct.IBAN, CreditIBAN: t.Tx.CdtrAcct.IBAN,
		DebtorBIC: t.DbtrAgt.BICFI, CreditBIC: t.Tx.CdtrAgt.BICFI,
	}, nil
}

// --- pain.002.001.11 (customer payment status report, response to pain.001) -----------------

type doc002pain struct {
	XMLName xml.Name    `xml:"Document"`
	NS      string      `xml:"xmlns,attr"`
	Body    cstmrStsRpt `xml:"CstmrPmtStsRpt"`
}

type cstmrStsRpt struct {
	GrpHdr   grpHdr002   `xml:"GrpHdr"`
	OrgnlGrp orgnlGrpSts `xml:"OrgnlGrpInfAndSts"`
	Tx       painSts     `xml:"OrgnlPmtInfAndSts>TxInfAndSts"`
}

type orgnlGrpSts struct {
	OrgnlMsgId   string `xml:"OrgnlMsgId"`
	OrgnlMsgNmId string `xml:"OrgnlMsgNmId"`
}

type painSts struct {
	OrgnlEndToEndId string  `xml:"OrgnlEndToEndId"`
	TxSts           string  `xml:"TxSts"`
	Rsn             *rsnInf `xml:"StsRsnInf,omitempty"`
}

func encode002pain(s Status) ([]byte, error) {
	b := cstmrStsRpt{
		GrpHdr:   grpHdr002{MsgId: s.MsgId, CreDtTm: nowISO()},
		OrgnlGrp: orgnlGrpSts{OrgnlMsgId: s.OrgnlEndToEndId, OrgnlMsgNmId: "pain.001.001.10"},
		Tx:       painSts{OrgnlEndToEndId: s.OrgnlEndToEndId, TxSts: s.TxSts},
	}
	if s.ReasonCode != "" {
		b.Tx.Rsn = &rsnInf{Code: s.ReasonCode}
	}
	return marshal(doc002pain{NS: nsPain002, Body: b}, KindPain002)
}

func decode002pain(raw []byte) (Status, error) {
	var d doc002pain
	if err := xml.Unmarshal(raw, &d); err != nil {
		return Status{}, fmt.Errorf("iso20022: parse pain.002: %w", err)
	}
	s := Status{Kind: KindPain002, MsgId: d.Body.GrpHdr.MsgId, OrgnlEndToEndId: d.Body.Tx.OrgnlEndToEndId, TxSts: d.Body.Tx.TxSts}
	if d.Body.Tx.Rsn != nil {
		s.ReasonCode = d.Body.Tx.Rsn.Code
	}
	return s, nil
}

func orToday(d string) string {
	if d != "" {
		return d
	}
	return time.Now().UTC().Format("2006-01-02")
}
