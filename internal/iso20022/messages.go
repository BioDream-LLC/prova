package iso20022

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

// The message kinds prova speaks, by their ISO 20022 short names.
const (
	KindPacs008 = "pacs.008" // FI-to-FI customer credit transfer
	KindPacs009 = "pacs.009" // financial institution credit transfer
	KindPacs003 = "pacs.003" // FI-to-FI customer direct debit
	KindPacs010 = "pacs.010" // financial institution direct debit
	KindPacs004 = "pacs.004" // payment return
	KindCamt056 = "camt.056" // FI-to-FI payment cancellation request
	KindPacs028 = "pacs.028" // FI-to-FI payment status request
	KindPacs002 = "pacs.002" // FI-to-FI payment status report (response)
	KindCamt029 = "camt.029" // resolution of investigation (response to camt.056)
	KindPain001 = "pain.001" // customer credit transfer initiation
	KindPain002 = "pain.002" // customer payment status report (response to pain.001)
	KindCamt060 = "camt.060" // account reporting request
	KindCamt052 = "camt.052" // bank-to-customer account report (intraday)
	KindCamt053 = "camt.053" // bank-to-customer statement
	KindCamt054 = "camt.054" // bank-to-customer debit/credit notification
	KindAdmi002 = "admi.002" // message reject
)

// versions is the message version prova writes for each kind; each matches a bundled schema.
var versions = map[string]string{
	KindPacs008: "001.08", KindPacs009: "001.08", KindPacs003: "001.08", KindPacs010: "001.04",
	KindPacs004: "001.10", KindCamt056: "001.08", KindPacs028: "001.03", KindPacs002: "001.10",
	KindCamt029: "001.09", KindPain001: "001.10", KindPain002: "001.11", KindCamt060: "001.05",
	KindCamt052: "001.08", KindCamt053: "001.08", KindCamt054: "001.08", KindAdmi002: "001.01",
}

func ns(kind string) string { return "urn:iso:std:iso:20022:tech:xsd:" + nameOf(kind) }

// rootKinds maps each message's business root element (the child of Document) to its kind.
var rootKinds = map[string]string{
	"FIToFICstmrCdtTrf":     KindPacs008,
	"FICdtTrf":              KindPacs009,
	"FIToFICstmrDrctDbt":    KindPacs003,
	"FIDrctDbt":             KindPacs010,
	"PmtRtr":                KindPacs004,
	"FIToFIPmtCxlReq":       KindCamt056,
	"FIToFIPmtStsReq":       KindPacs028,
	"FIToFIPmtStsRpt":       KindPacs002,
	"RsltnOfInvstgtn":       KindCamt029,
	"CstmrCdtTrfInitn":      KindPain001,
	"CstmrPmtStsRpt":        KindPain002,
	"AcctRptgReq":           KindCamt060,
	"BkToCstmrAcctRpt":      KindCamt052,
	"BkToCstmrStmt":         KindCamt053,
	"BkToCstmrDbtCdtNtfctn": KindCamt054,
	"admi.002.001.01":       KindAdmi002,
}

// RequestKinds lists every ISO 20022 message prova sends, in the order it offers them.
func RequestKinds() []string {
	return []string{KindPacs008, KindPacs009, KindPacs003, KindPacs010, KindPacs004, KindCamt056, KindPacs028, KindPain001, KindCamt060}
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

// --- pacs.009.001.08 (financial institution credit transfer) --------------------------------

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
	PmtId    pmtId     `xml:"PmtId"`
	PmtTpInf *pmtTpInf `xml:"PmtTpInf,omitempty"`
	Amt      amt       `xml:"IntrBkSttlmAmt"`
	SttlmDt  string    `xml:"IntrBkSttlmDt,omitempty"`
	Dbtr     agent     `xml:"Dbtr"`
	DbtrAcct acct      `xml:"DbtrAcct"`
	Cdtr     agent     `xml:"Cdtr"`
	CdtrAcct acct      `xml:"CdtrAcct"`
}

func encode009(r Request) ([]byte, error) {
	return marshal(doc009{NS: ns(KindPacs009), Body: fiCdtTrf{
		GrpHdr: grpHdr008{MsgId: r.MsgId, CreDtTm: nowISO(), NbOfTxs: "1", SttlmInf: sttlm{Mtd: "CLRG"}},
		Tx: fiTxInf{
			PmtId:    pmtId{InstrId: r.MsgId, EndToEndId: r.EndToEndId, TxId: r.EndToEndId, UETR: r.uetr()},
			PmtTpInf: paymentType(r.ServiceLevel, r.LocalInstrument, ""),
			Amt:      amt{Ccy: r.Currency, Val: r.Amount},
			SttlmDt:  r.ValueDate,
			Dbtr:     fiAgent(r.DebtorBIC, r.DebtorRTN),
			DbtrAcct: account(r.DebtorIBAN, r.DebtorAcct),
			Cdtr:     fiAgent(r.CreditBIC, r.CreditRTN),
			CdtrAcct: account(r.CreditIBAN, r.CreditAcct),
		},
	}}, KindPacs009)
}

func parse009(raw []byte) (Request, error) {
	var d doc009
	if err := unmarshal(raw, &d, KindPacs009); err != nil {
		return Request{}, err
	}
	t := d.Body.Tx
	return Request{
		Kind: KindPacs009, MsgId: d.Body.GrpHdr.MsgId, EndToEndId: t.PmtId.EndToEndId, UETR: t.PmtId.UETR,
		Amount: trim(t.Amt.Val), Currency: t.Amt.Ccy, ValueDate: trim(t.SttlmDt),
		DebtorAcct: t.DbtrAcct.id(), CreditAcct: t.CdtrAcct.id(), DebtorIBAN: t.DbtrAcct.Id.IBAN, CreditIBAN: t.CdtrAcct.Id.IBAN,
		DebtorBIC: t.Dbtr.bic(), CreditBIC: t.Cdtr.bic(), DebtorRTN: t.Dbtr.rtn(), CreditRTN: t.Cdtr.rtn(),
	}, nil
}

// --- pacs.003.001.08 (FI-to-FI customer direct debit) ---------------------------------------
//
// The creditor's agent asks the debtor's agent (the host) to collect from the debtor's account under a
// mandate the debtor signed. The host must refuse a collection with no valid mandate (MD01).

type doc003 struct {
	XMLName xml.Name `xml:"Document"`
	NS      string   `xml:"xmlns,attr"`
	Body    drctDbt  `xml:"FIToFICstmrDrctDbt"`
}

type drctDbt struct {
	GrpHdr grpHdr008 `xml:"GrpHdr"`
	Tx     ddTxInf   `xml:"DrctDbtTxInf"`
}

type ddTxInf struct {
	PmtId    pmtId     `xml:"PmtId"`
	PmtTpInf *pmtTpInf `xml:"PmtTpInf,omitempty"`
	Amt      amt       `xml:"IntrBkSttlmAmt"`
	SttlmDt  string    `xml:"IntrBkSttlmDt,omitempty"`
	ChrgBr   string    `xml:"ChrgBr"`
	ColltnDt string    `xml:"ReqdColltnDt,omitempty"`
	DrctDbt  *ddTx     `xml:"DrctDbtTx,omitempty"`
	Cdtr     party     `xml:"Cdtr"`
	CdtrAcct acct      `xml:"CdtrAcct"`
	CdtrAgt  agent     `xml:"CdtrAgt"`
	Dbtr     party     `xml:"Dbtr"`
	DbtrAcct acct      `xml:"DbtrAcct"`
	DbtrAgt  agent     `xml:"DbtrAgt"`
	RmtInf   *rmtInf   `xml:"RmtInf,omitempty"`
}

type ddTx struct {
	Mndt        *mndt     `xml:"MndtRltdInf,omitempty"`
	CdtrSchmeId *schemeID `xml:"CdtrSchmeId,omitempty"`
}

type mndt struct {
	MndtId    string `xml:"MndtId,omitempty"`
	DtOfSgntr string `xml:"DtOfSgntr,omitempty"`
}

// schemeID is the creditor identifier (SEPA: the creditor's scheme id), carried as a private
// identification with scheme name SEPA.
type schemeID struct {
	Id struct {
		PrvtId struct {
			Othr struct {
				Id      string `xml:"Id"`
				SchmeNm struct {
					Prtry string `xml:"Prtry"`
				} `xml:"SchmeNm"`
			} `xml:"Othr"`
		} `xml:"PrvtId"`
	} `xml:"Id"`
}

func encode003(r Request) ([]byte, error) {
	tx := ddTxInf{
		PmtId:    pmtId{InstrId: r.MsgId, EndToEndId: r.EndToEndId, TxId: r.EndToEndId, UETR: r.uetr()},
		PmtTpInf: paymentType(r.ServiceLevel, r.LocalInstrument, r.SequenceType),
		Amt:      amt{Ccy: r.Currency, Val: r.Amount},
		SttlmDt:  r.ValueDate,
		ChrgBr:   r.ChargeBearer,
		Cdtr:     person(r.CreditorName, r.CreditBIC),
		CdtrAcct: account(r.CreditIBAN, r.CreditAcct),
		CdtrAgt:  fiAgent(r.CreditBIC, r.CreditRTN),
		Dbtr:     person(r.DebtorName, r.DebtorBIC),
		DbtrAcct: account(r.DebtorIBAN, r.DebtorAcct),
		DbtrAgt:  fiAgent(r.DebtorBIC, r.DebtorRTN),
		RmtInf:   remittance(r.Remittance),
	}
	if r.MandateId != "" || r.CreditorSchemeId != "" {
		tx.DrctDbt = &ddTx{}
		if r.MandateId != "" {
			tx.DrctDbt.Mndt = &mndt{MndtId: r.MandateId, DtOfSgntr: r.MandateDate}
		}
		if r.CreditorSchemeId != "" {
			s := &schemeID{}
			s.Id.PrvtId.Othr.Id = r.CreditorSchemeId
			s.Id.PrvtId.Othr.SchmeNm.Prtry = "SEPA"
			tx.DrctDbt.CdtrSchmeId = s
		}
	}
	return marshal(doc003{NS: ns(KindPacs003), Body: drctDbt{
		GrpHdr: grpHdr008{MsgId: r.MsgId, CreDtTm: nowISO(), NbOfTxs: "1", SttlmInf: sttlm{Mtd: "CLRG"}},
		Tx:     tx,
	}}, KindPacs003)
}

func parse003(raw []byte) (Request, error) {
	var d doc003
	if err := unmarshal(raw, &d, KindPacs003); err != nil {
		return Request{}, err
	}
	t := d.Body.Tx
	r := Request{
		Kind: KindPacs003, MsgId: d.Body.GrpHdr.MsgId, EndToEndId: t.PmtId.EndToEndId, UETR: t.PmtId.UETR,
		Amount: trim(t.Amt.Val), Currency: t.Amt.Ccy, ValueDate: trim(t.SttlmDt), ChargeBearer: t.ChrgBr,
		DebtorAcct: t.DbtrAcct.id(), CreditAcct: t.CdtrAcct.id(), DebtorIBAN: t.DbtrAcct.Id.IBAN, CreditIBAN: t.CdtrAcct.Id.IBAN,
		DebtorBIC: t.DbtrAgt.bic(), CreditBIC: t.CdtrAgt.bic(), DebtorName: t.Dbtr.Nm, CreditorName: t.Cdtr.Nm,
	}
	if t.DrctDbt != nil {
		if t.DrctDbt.Mndt != nil {
			r.MandateId, r.MandateDate = t.DrctDbt.Mndt.MndtId, t.DrctDbt.Mndt.DtOfSgntr
		}
		if t.DrctDbt.CdtrSchmeId != nil {
			r.CreditorSchemeId = t.DrctDbt.CdtrSchmeId.Id.PrvtId.Othr.Id
		}
	}
	if t.PmtTpInf != nil {
		r.SequenceType = t.PmtTpInf.SeqTp
	}
	return r, nil
}

// --- pacs.010.001.04 (financial institution direct debit) -----------------------------------
//
// One institution debits another's account at the host under a bilateral agreement: no customer
// mandate, but funds, account state and duplicates still apply.

type doc010 struct {
	XMLName xml.Name  `xml:"Document"`
	NS      string    `xml:"xmlns,attr"`
	Body    fiDrctDbt `xml:"FIDrctDbt"`
}

type fiDrctDbt struct {
	GrpHdr  grpHdr010 `xml:"GrpHdr"`
	CdtInst cdtInstr  `xml:"CdtInstr"`
}

type grpHdr010 struct {
	MsgId   string `xml:"MsgId"`
	CreDtTm string `xml:"CreDtTm"`
	NbOfTxs string `xml:"NbOfTxs"`
}

type cdtInstr struct {
	CdtId    string    `xml:"CdtId"`
	SttlmDt  string    `xml:"IntrBkSttlmDt,omitempty"`
	Cdtr     agent     `xml:"Cdtr"`
	CdtrAcct acct      `xml:"CdtrAcct"`
	Tx       fiDDTxInf `xml:"DrctDbtTxInf"`
}

type fiDDTxInf struct {
	PmtId    pmtId `xml:"PmtId"`
	Amt      amt   `xml:"IntrBkSttlmAmt"`
	Dbtr     agent `xml:"Dbtr"`
	DbtrAcct acct  `xml:"DbtrAcct"`
}

func encode010(r Request) ([]byte, error) {
	return marshal(doc010{NS: ns(KindPacs010), Body: fiDrctDbt{
		GrpHdr: grpHdr010{MsgId: r.MsgId, CreDtTm: nowISO(), NbOfTxs: "1"},
		CdtInst: cdtInstr{
			CdtId: r.MsgId, SttlmDt: r.ValueDate,
			Cdtr: fiAgent(r.CreditBIC, r.CreditRTN), CdtrAcct: account(r.CreditIBAN, r.CreditAcct),
			Tx: fiDDTxInf{
				PmtId: pmtId{InstrId: r.MsgId, EndToEndId: r.EndToEndId, TxId: r.EndToEndId, UETR: r.uetr()},
				Amt:   amt{Ccy: r.Currency, Val: r.Amount},
				Dbtr:  fiAgent(r.DebtorBIC, r.DebtorRTN), DbtrAcct: account(r.DebtorIBAN, r.DebtorAcct),
			},
		},
	}}, KindPacs010)
}

func parse010(raw []byte) (Request, error) {
	var d doc010
	if err := unmarshal(raw, &d, KindPacs010); err != nil {
		return Request{}, err
	}
	c := d.Body.CdtInst
	t := c.Tx
	return Request{
		Kind: KindPacs010, MsgId: d.Body.GrpHdr.MsgId, EndToEndId: t.PmtId.EndToEndId, UETR: t.PmtId.UETR,
		Amount: trim(t.Amt.Val), Currency: t.Amt.Ccy, ValueDate: trim(c.SttlmDt),
		DebtorAcct: t.DbtrAcct.id(), CreditAcct: c.CdtrAcct.id(), DebtorIBAN: t.DbtrAcct.Id.IBAN, CreditIBAN: c.CdtrAcct.Id.IBAN,
		DebtorBIC: t.Dbtr.bic(), CreditBIC: c.Cdtr.bic(),
	}, nil
}

// --- pacs.004.001.10 (payment return) -------------------------------------------------------

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
	ChrgBr          string `xml:"ChrgBr,omitempty"`
	RtrRsn          *rsnCd `xml:"RtrRsnInf,omitempty"`
}

func encode004(r Request) ([]byte, error) {
	return marshal(doc004{NS: ns(KindPacs004), Body: pmtRtr{
		GrpHdr: grpHdr008{MsgId: r.MsgId, CreDtTm: nowISO(), NbOfTxs: "1", SttlmInf: sttlm{Mtd: "CLRG"}},
		Tx: rtrTxInf{RtrId: r.EndToEndId, OrgnlEndToEndId: r.OrgnlEndToEndId,
			RtrdAmt: amt{Ccy: r.Currency, Val: r.Amount}, ChrgBr: r.ChargeBearer, RtrRsn: reason(r.Reason)},
	}}, KindPacs004)
}

func parse004(raw []byte) (Request, error) {
	var d doc004
	if err := unmarshal(raw, &d, KindPacs004); err != nil {
		return Request{}, err
	}
	t := d.Body.Tx
	return Request{
		Kind: KindPacs004, MsgId: d.Body.GrpHdr.MsgId, EndToEndId: t.RtrId, OrgnlEndToEndId: t.OrgnlEndToEndId,
		Amount: trim(t.RtrdAmt.Val), Currency: t.RtrdAmt.Ccy, Reason: t.RtrRsn.code(),
	}, nil
}

// --- camt.056.001.08 (FI-to-FI payment cancellation request) --------------------------------

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
	CxlRsn          *rsnCd `xml:"CxlRsnInf,omitempty"`
}

func encode056(r Request) ([]byte, error) {
	tx := cxlTxInf{CxlId: r.EndToEndId, OrgnlEndToEndId: r.OrgnlEndToEndId, CxlRsn: reason(r.Reason)}
	if r.Amount != "" {
		tx.OrgnlAmt = &amt{Ccy: r.Currency, Val: r.Amount}
	}
	return marshal(doc056{NS: ns(KindCamt056), Body: cxlReq{
		Assgnmt: assignment{Id: r.MsgId, Assgnr: fiAgent(r.DebtorBIC, ""), Assgne: fiAgent(r.CreditBIC, ""), CreDtTm: nowISO()},
		Tx:      tx,
	}}, KindCamt056)
}

func parse056(raw []byte) (Request, error) {
	var d doc056
	if err := unmarshal(raw, &d, KindCamt056); err != nil {
		return Request{}, err
	}
	t := d.Body.Tx
	r := Request{
		Kind: KindCamt056, MsgId: d.Body.Assgnmt.Id, EndToEndId: t.CxlId, OrgnlEndToEndId: t.OrgnlEndToEndId,
		Reason: t.CxlRsn.code(), DebtorBIC: d.Body.Assgnmt.Assgnr.bic(), CreditBIC: d.Body.Assgnmt.Assgne.bic(),
	}
	if t.OrgnlAmt != nil {
		r.Amount, r.Currency = trim(t.OrgnlAmt.Val), t.OrgnlAmt.Ccy
	}
	return r, nil
}

// --- pacs.028.001.03 (FI-to-FI payment status request) --------------------------------------

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
	return marshal(doc028{NS: ns(KindPacs028), Body: stsReq{
		GrpHdr: grpHdr002{MsgId: r.MsgId, CreDtTm: nowISO()},
		Tx:     stsTxInf{StsReqId: r.EndToEndId, OrgnlEndToEndId: r.OrgnlEndToEndId},
	}}, KindPacs028)
}

func parse028(raw []byte) (Request, error) {
	var d doc028
	if err := unmarshal(raw, &d, KindPacs028); err != nil {
		return Request{}, err
	}
	return Request{Kind: KindPacs028, MsgId: d.Body.GrpHdr.MsgId, EndToEndId: d.Body.Tx.StsReqId, OrgnlEndToEndId: d.Body.Tx.OrgnlEndToEndId}, nil
}

// --- camt.029.001.09 (resolution of investigation, the reply to camt.056) -------------------

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
	Rsn             *rsnCd `xml:"CxlStsRsnInf,omitempty"`
}

// encode029 writes the resolution. Sts/Conf carries prova's decision code (CNCL cancelled, RJCR
// refused); the transaction's cancellation status uses the code list the schema allows (ACCR accepted,
// RJCR rejected).
func encode029(s Status) ([]byte, error) {
	txs := "RJCR"
	if s.TxSts == "CNCL" {
		txs = "ACCR"
	}
	return marshal(doc029{NS: ns(KindCamt029), Body: rsltn{
		Assgnmt: assignment{Id: s.MsgId, Assgnr: fiAgent("BANKFRPPXXX", ""), Assgne: fiAgent("BANKDEFFXXX", ""), CreDtTm: nowISO()},
		Conf:    s.TxSts,
		Tx:      cxlTxSts{OrgnlEndToEndId: s.OrgnlEndToEndId, TxCxlSts: txs, Rsn: reason(s.ReasonCode)},
	}}, KindCamt029)
}

func decode029(raw []byte) (Status, error) {
	var d doc029
	if err := unmarshal(raw, &d, KindCamt029); err != nil {
		return Status{}, err
	}
	return Status{Kind: KindCamt029, MsgId: d.Body.Assgnmt.Id, OrgnlEndToEndId: d.Body.Tx.OrgnlEndToEndId,
		TxSts: d.Body.Conf, ReasonCode: d.Body.Tx.Rsn.code()}, nil
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

type pmtInf struct {
	PmtInfId  string    `xml:"PmtInfId"`
	PmtMtd    string    `xml:"PmtMtd"`
	PmtTpInf  *pmtTpInf `xml:"PmtTpInf,omitempty"`
	ReqdExctn reqdDt    `xml:"ReqdExctnDt"`
	Dbtr      party     `xml:"Dbtr"`
	DbtrAcct  acct      `xml:"DbtrAcct"`
	DbtrAgt   agent     `xml:"DbtrAgt"`
	ChrgBr    string    `xml:"ChrgBr,omitempty"`
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
	RmtInf   *rmtInf  `xml:"RmtInf,omitempty"`
}

type instdAmt struct {
	InstdAmt amt `xml:"InstdAmt"`
}

func encode001(r Request) ([]byte, error) {
	return marshal(doc001{NS: ns(KindPain001), Body: cstmrCT{
		GrpHdr: grpHdrPain{MsgId: r.MsgId, CreDtTm: nowISO(), NbOfTxs: "1", InitgPty: party{Nm: r.DebtorName}},
		PmtInf: pmtInf{
			PmtInfId: r.MsgId, PmtMtd: "TRF", PmtTpInf: paymentType(r.ServiceLevel, r.LocalInstrument, ""),
			ReqdExctn: reqdDt{Dt: orToday(r.ValueDate)},
			Dbtr:      person(r.DebtorName, r.DebtorBIC), DbtrAcct: account(r.DebtorIBAN, r.DebtorAcct), DbtrAgt: fiAgent(r.DebtorBIC, r.DebtorRTN),
			ChrgBr: r.ChargeBearer,
			Tx: painTxInf{
				PmtId:   pmtId{InstrId: r.MsgId, EndToEndId: r.EndToEndId, UETR: r.uetr()},
				Amt:     instdAmt{InstdAmt: amt{Ccy: r.Currency, Val: r.Amount}},
				CdtrAgt: fiAgent(r.CreditBIC, r.CreditRTN), Cdtr: person(r.CreditorName, r.CreditBIC), CdtrAcct: account(r.CreditIBAN, r.CreditAcct),
				RmtInf: remittance(r.Remittance),
			},
		},
	}}, KindPain001)
}

func parse001(raw []byte) (Request, error) {
	var d doc001
	if err := unmarshal(raw, &d, KindPain001); err != nil {
		return Request{}, err
	}
	t := d.Body.PmtInf
	return Request{
		Kind: KindPain001, MsgId: d.Body.GrpHdr.MsgId, EndToEndId: t.Tx.PmtId.EndToEndId, UETR: t.Tx.PmtId.UETR,
		Amount: trim(t.Tx.Amt.InstdAmt.Val), Currency: t.Tx.Amt.InstdAmt.Ccy, ValueDate: t.ReqdExctn.Dt,
		DebtorAcct: t.DbtrAcct.id(), CreditAcct: t.Tx.CdtrAcct.id(), DebtorIBAN: t.DbtrAcct.Id.IBAN, CreditIBAN: t.Tx.CdtrAcct.Id.IBAN,
		DebtorBIC: t.DbtrAgt.bic(), CreditBIC: t.Tx.CdtrAgt.bic(), DebtorName: t.Dbtr.Nm, CreditorName: t.Tx.Cdtr.Nm,
	}, nil
}

// --- pain.002.001.11 (customer payment status report, the reply to pain.001) ----------------

type doc002pain struct {
	XMLName xml.Name    `xml:"Document"`
	NS      string      `xml:"xmlns,attr"`
	Body    cstmrStsRpt `xml:"CstmrPmtStsRpt"`
}

type cstmrStsRpt struct {
	GrpHdr   grpHdr002   `xml:"GrpHdr"`
	OrgnlGrp orgnlGrpInf `xml:"OrgnlGrpInfAndSts"`
	PmtInf   painPmtSts  `xml:"OrgnlPmtInfAndSts"`
}

type painPmtSts struct {
	OrgnlPmtInfId string  `xml:"OrgnlPmtInfId"`
	Tx            painSts `xml:"TxInfAndSts"`
}

type painSts struct {
	OrgnlEndToEndId string `xml:"OrgnlEndToEndId"`
	TxSts           string `xml:"TxSts"`
	Rsn             *rsnCd `xml:"StsRsnInf,omitempty"`
}

func encode002pain(s Status) ([]byte, error) {
	orig := orDefault(s.OrgnlMsgId, s.OrgnlEndToEndId)
	return marshal(doc002pain{NS: ns(KindPain002), Body: cstmrStsRpt{
		GrpHdr:   grpHdr002{MsgId: s.MsgId, CreDtTm: nowISO()},
		OrgnlGrp: orgnlGrpInf{OrgnlMsgId: orig, OrgnlMsgNmId: nameOf(KindPain001)},
		PmtInf: painPmtSts{OrgnlPmtInfId: orig,
			Tx: painSts{OrgnlEndToEndId: s.OrgnlEndToEndId, TxSts: s.TxSts, Rsn: reason(s.ReasonCode)}},
	}}, KindPain002)
}

func decode002pain(raw []byte) (Status, error) {
	var d doc002pain
	if err := unmarshal(raw, &d, KindPain002); err != nil {
		return Status{}, err
	}
	t := d.Body.PmtInf.Tx
	return Status{Kind: KindPain002, MsgId: d.Body.GrpHdr.MsgId, OrgnlMsgId: d.Body.OrgnlGrp.OrgnlMsgId,
		OrgnlEndToEndId: t.OrgnlEndToEndId, TxSts: t.TxSts, ReasonCode: t.Rsn.code()}, nil
}

// --- camt.060.001.05 (account reporting request) --------------------------------------------

type doc060 struct {
	XMLName xml.Name   `xml:"Document"`
	NS      string     `xml:"xmlns,attr"`
	Body    acctRptReq `xml:"AcctRptgReq"`
}

type acctRptReq struct {
	GrpHdr grpHdr002 `xml:"GrpHdr"`
	Req    rptgReq   `xml:"RptgReq"`
}

type rptgReq struct {
	Id       string   `xml:"Id,omitempty"`
	ReqdMsg  string   `xml:"ReqdMsgNmId"`
	Acct     acct     `xml:"Acct"`
	AcctOwnr acctOwnr `xml:"AcctOwnr"`
	RptgPrd  *rptgPrd `xml:"RptgPrd,omitempty"`
}

type acctOwnr struct {
	Pty party `xml:"Pty"`
}

type rptgPrd struct {
	FrToDt struct {
		FrDt string `xml:"FrDt"`
		ToDt string `xml:"ToDt,omitempty"`
	} `xml:"FrToDt"`
	Tp string `xml:"Tp"`
}

func encode060(r Request) ([]byte, error) {
	req := rptgReq{Id: r.EndToEndId, ReqdMsg: nameOf(orDefault(r.ReportKind, KindCamt053)),
		Acct: account("", r.Account), AcctOwnr: acctOwnr{Pty: party{Nm: r.DebtorName}}}
	if r.FromDate != "" {
		p := &rptgPrd{Tp: "ALLL"}
		p.FrToDt.FrDt, p.FrToDt.ToDt = r.FromDate, r.ToDate
		req.RptgPrd = p
	}
	return marshal(doc060{NS: ns(KindCamt060), Body: acctRptReq{GrpHdr: grpHdr002{MsgId: r.MsgId, CreDtTm: nowISO()}, Req: req}}, KindCamt060)
}

func parse060(raw []byte) (Request, error) {
	var d doc060
	if err := unmarshal(raw, &d, KindCamt060); err != nil {
		return Request{}, err
	}
	q := d.Body.Req
	kind := q.ReqdMsg
	if len(kind) >= 8 {
		kind = kind[:8] // "camt.053.001.08" -> "camt.053"
	}
	r := Request{Kind: KindCamt060, MsgId: d.Body.GrpHdr.MsgId, EndToEndId: q.Id, Account: q.Acct.id(), ReportKind: kind}
	if q.RptgPrd != nil {
		r.FromDate, r.ToDate = q.RptgPrd.FrToDt.FrDt, q.RptgPrd.FrToDt.ToDt
	}
	return r, nil
}

// --- camt.052 / camt.053 / camt.054 (account reports) ---------------------------------------
//
// The three share a shape: a group header, then one report (Rpt), statement (Stmt) or notification
// (Ntfctn) for an account, with balances (not in camt.054) and entries.

type report struct {
	Id   string    `xml:"Id"`
	Acct acct      `xml:"Acct"`
	Bal  []balance `xml:"Bal,omitempty"`
	Ntry []entry   `xml:"Ntry,omitempty"`
}

type balance struct {
	Tp struct {
		CdOrPrtry code `xml:"CdOrPrtry"`
	} `xml:"Tp"`
	Amt       amt    `xml:"Amt"`
	CdtDbtInd string `xml:"CdtDbtInd"`
	Dt        struct {
		Dt string `xml:"Dt"`
	} `xml:"Dt"`
}

type entry struct {
	Amt       amt    `xml:"Amt"`
	CdtDbtInd string `xml:"CdtDbtInd"`
	Sts       code   `xml:"Sts"`
	BkTxCd    struct {
		Prtry struct {
			Cd string `xml:"Cd"`
		} `xml:"Prtry"`
	} `xml:"BkTxCd"`
}

type reportBody struct {
	GrpHdr grpHdr002 `xml:"GrpHdr"`
	Rpt    *report   `xml:"Rpt,omitempty"`
	Stmt   *report   `xml:"Stmt,omitempty"`
	Ntfctn *report   `xml:"Ntfctn,omitempty"`
}

type docReport struct {
	XMLName xml.Name    `xml:"Document"`
	NS      string      `xml:"xmlns,attr"`
	Acct    *reportBody `xml:"BkToCstmrAcctRpt,omitempty"`
	Stmt    *reportBody `xml:"BkToCstmrStmt,omitempty"`
	Ntfctn  *reportBody `xml:"BkToCstmrDbtCdtNtfctn,omitempty"`
}

// encodeReport writes the account report s.Kind names. Statements and intraday reports carry the
// closing booked balance (CLBD); a notification carries entries only. Entries are one booked credit per
// count, for one minor unit, so a test can count them.
func encodeReport(s Status) ([]byte, error) {
	r := &report{Id: s.MsgId, Acct: account("", s.Account)}
	if s.Kind != KindCamt054 {
		var b balance
		b.Tp.CdOrPrtry.Cd = "CLBD"
		v, ind := s.ClosingBalance, "CRDT"
		if strings.HasPrefix(v, "-") {
			v, ind = v[1:], "DBIT"
		}
		b.Amt, b.CdtDbtInd = amt{Ccy: s.Currency, Val: v}, ind
		b.Dt.Dt = today()
		r.Bal = []balance{b}
	}
	for i := 0; i < s.Entries; i++ {
		var e entry
		e.Amt, e.CdtDbtInd, e.Sts.Cd = amt{Ccy: s.Currency, Val: "0.01"}, "CRDT", "BOOK"
		e.BkTxCd.Prtry.Cd = "PROVA"
		r.Ntry = append(r.Ntry, e)
	}
	body := &reportBody{GrpHdr: grpHdr002{MsgId: s.MsgId, CreDtTm: nowISO()}}
	d := docReport{NS: ns(s.Kind)}
	switch s.Kind {
	case KindCamt052:
		body.Rpt, d.Acct = r, body
	case KindCamt054:
		body.Ntfctn, d.Ntfctn = r, body
	default:
		body.Stmt, d.Stmt = r, body
	}
	return marshal(d, s.Kind)
}

func decodeReport(raw []byte) (Status, error) {
	var d docReport
	kind := Kind(raw)
	if err := unmarshal(raw, &d, kind); err != nil {
		return Status{}, err
	}
	var body *reportBody
	var r *report
	switch {
	case d.Acct != nil:
		body, r = d.Acct, d.Acct.Rpt
	case d.Stmt != nil:
		body, r = d.Stmt, d.Stmt.Stmt
	case d.Ntfctn != nil:
		body, r = d.Ntfctn, d.Ntfctn.Ntfctn
	}
	if body == nil || r == nil {
		return Status{}, fmt.Errorf("iso20022: %s carries no report", kind)
	}
	s := Status{Kind: kind, MsgId: body.GrpHdr.MsgId, Account: r.Acct.id(), Entries: len(r.Ntry)}
	for _, b := range r.Bal {
		if b.Tp.CdOrPrtry.Cd == "CLBD" {
			s.Currency, s.ClosingBalance = b.Amt.Ccy, trim(b.Amt.Val)
			if b.CdtDbtInd == "DBIT" {
				s.ClosingBalance = "-" + s.ClosingBalance
			}
		}
	}
	if s.Currency == "" && len(r.Ntry) > 0 {
		s.Currency = r.Ntry[0].Amt.Ccy
	}
	return s, nil
}

// --- admi.002.001.01 (message reject) -------------------------------------------------------

type docAdmi002 struct {
	XMLName xml.Name `xml:"Document"`
	NS      string   `xml:"xmlns,attr"`
	Body    struct {
		RltdRef struct {
			Ref string `xml:"Ref"`
		} `xml:"RltdRef"`
		Rsn struct {
			RjctgPtyRsn string `xml:"RjctgPtyRsn"`
			RjctnDtTm   string `xml:"RjctnDtTm,omitempty"`
			RsnDesc     string `xml:"RsnDesc,omitempty"`
		} `xml:"Rsn"`
	} `xml:"admi.002.001.01"`
}

func encodeAdmi002(s Status) ([]byte, error) {
	var d docAdmi002
	d.NS = ns(KindAdmi002)
	d.Body.RltdRef.Ref = orDefault(s.OrgnlMsgId, s.OrgnlEndToEndId)
	d.Body.Rsn.RjctgPtyRsn = s.ReasonCode
	d.Body.Rsn.RjctnDtTm = nowISO()
	return marshal(d, KindAdmi002)
}

func decodeAdmi002(raw []byte) (Status, error) {
	var d docAdmi002
	if err := unmarshal(raw, &d, KindAdmi002); err != nil {
		return Status{}, err
	}
	return Status{Kind: KindAdmi002, OrgnlMsgId: d.Body.RltdRef.Ref, ReasonCode: d.Body.Rsn.RjctgPtyRsn}, nil
}

// signedMinor renders minor units as a signed two-decimal string.
func signedMinor(v int64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	s := strconv.FormatInt(v/100, 10) + "." + fmt.Sprintf("%02d", v%100)
	if neg {
		return "-" + s
	}
	return s
}
