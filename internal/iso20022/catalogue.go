package iso20022

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/biodream-llc/prova/internal/validate"
)

// Catalogue messages: any message version in the ISO 20022 catalogue, beyond the ones prova speaks
// natively. prova sends the instance a scenario names (a generated valid sample, or a deliberately
// broken variant), and a host answers as a network gateway does: an admi.007 receipt acknowledgement
// for a schema-valid message, an admi.002 message reject naming the first schema error otherwise.

// KindAdmi007 is the receipt acknowledgement a gateway returns for a message it accepted.
const KindAdmi007 = "admi.007"

const nsAdmi007 = "urn:iso:std:iso:20022:tech:xsd:admi.007.001.01"

// IsCatalogue reports whether a message kind is a catalogue message rather than one prova encodes
// natively: a full version (sese.023.001.09) or a message name (sese.023) that prova does not speak.
func IsCatalogue(kind string) bool {
	if kind == "" {
		return false
	}
	for _, k := range RequestKinds() {
		if kind == k || kind == k+"."+versions[k] {
			return false
		}
	}
	_, ok := validate.LookupMessage(kind)
	return ok
}

// encodeCatalogue builds the instance of a catalogue message a request names: Variant chooses it
// (minimal by default; see validate.VariantNames), or XML supplies it outright.
func encodeCatalogue(req Request) ([]byte, error) {
	if req.XML != "" {
		return []byte(req.XML), nil
	}
	m, ok := validate.LookupMessage(req.Kind)
	if !ok {
		return nil, fmt.Errorf("iso20022: %s is not in the ISO 20022 catalogue", req.Kind)
	}
	v := req.Variant
	if v == "" {
		v = "minimal"
	}
	raw, err := validate.Variant(m.Kind, v)
	if err != nil {
		return nil, err
	}
	if req.MsgId != "" {
		raw = setFirst(raw, "MsgId", req.MsgId)
	}
	return raw, nil
}

// setFirst replaces the text of the first simple element of that name, when its value is plain text.
func setFirst(raw []byte, name, value string) []byte {
	open := []byte("<" + name + ">")
	i := bytes.Index(raw, open)
	if i < 0 {
		return raw
	}
	j := bytes.Index(raw[i:], []byte("</"+name+">"))
	if j < 0 || bytes.ContainsRune(raw[i+len(open):i+j], '<') {
		return raw
	}
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(value))
	return append(append(append([]byte{}, raw[:i+len(open)]...), b.Bytes()...), raw[i+j:]...)
}

// catalogueKind reads a message's version from the namespace of its Document.
func catalogueKind(raw []byte) string {
	d := xml.NewDecoder(bytes.NewReader(raw))
	for {
		tok, err := d.Token()
		if err != nil {
			return ""
		}
		if se, ok := tok.(xml.StartElement); ok {
			if k := validate.KindForNamespace(se.Name.Space); k != "" {
				return k
			}
			return strings.TrimPrefix(se.Name.Space, "urn:iso:std:iso:20022:tech:xsd:")
		}
	}
}

// parseCatalogue reads what the gateway needs from a catalogue message: its kind and first MsgId.
func parseCatalogue(raw []byte) (Request, error) {
	k := catalogueKind(raw)
	if _, ok := validate.LookupMessage(k); !ok {
		return Request{}, fmt.Errorf("iso20022: unsupported message %q", k)
	}
	req := Request{Kind: k, XML: string(raw)}
	d := xml.NewDecoder(bytes.NewReader(raw))
	inMsgId := false
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			inMsgId = t.Name.Local == "MsgId" || t.Name.Local == "Id" && req.MsgId == ""
		case xml.CharData:
			if inMsgId && req.MsgId == "" && strings.TrimSpace(string(t)) != "" {
				req.MsgId = strings.TrimSpace(string(t))
			}
		case xml.EndElement:
			inMsgId = false
		}
		if req.MsgId != "" {
			break
		}
	}
	if req.MsgId == "" {
		req.MsgId = "NOREF"
	}
	return req, nil
}

// gatewayReply answers a catalogue message: the compliant gateway validates it against its schema,
// the defective one acknowledges whatever it is sent.
func (s *Sim) gatewayReply(req Request) Status {
	ref := truncate(req.MsgId, 35)
	if s.Mode != "accept-all" {
		rep, err := validate.Message([]byte(req.XML), "")
		if err != nil {
			return Status{Kind: KindAdmi002, MsgId: "REJ-" + ref, OrgnlMsgId: ref, ReasonCode: "XMLSyntax", Description: truncate(err.Error(), 350)}
		}
		for _, f := range rep.Findings {
			if f.Severity == validate.Error {
				return Status{Kind: KindAdmi002, MsgId: "REJ-" + ref, OrgnlMsgId: ref, ReasonCode: "SchemaValidation",
					Location: truncate(f.Path, 350), Description: truncate(f.Message, 350)}
			}
		}
	}
	return Status{Kind: KindAdmi007, MsgId: "ACK-" + ref, OrgnlMsgId: ref, OrgnlMsgNmId: req.Kind, TxSts: "RCVD"}
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// --- admi.007.001.01 (receipt acknowledgement) ----------------------------------------------

type docAdmi007 struct {
	XMLName xml.Name `xml:"Document"`
	NS      string   `xml:"xmlns,attr"`
	Body    struct {
		MsgId struct {
			MsgId   string `xml:"MsgId"`
			CreDtTm string `xml:"CreDtTm,omitempty"`
		} `xml:"MsgId"`
		Rpt struct {
			RltdRef struct {
				Ref   string `xml:"Ref"`
				MsgNm string `xml:"MsgNm,omitempty"`
			} `xml:"RltdRef"`
			ReqHdlg struct {
				StsCd string `xml:"StsCd"`
				Desc  string `xml:"Desc,omitempty"`
			} `xml:"ReqHdlg"`
		} `xml:"Rpt"`
	} `xml:"RctAck"`
}

func encodeAdmi007(s Status) ([]byte, error) {
	var d docAdmi007
	d.NS = nsAdmi007
	d.Body.MsgId.MsgId = truncate(s.MsgId, 35)
	d.Body.MsgId.CreDtTm = nowISO()
	d.Body.Rpt.RltdRef.Ref = truncate(orDefault(s.OrgnlMsgId, "NOREF"), 35)
	d.Body.Rpt.RltdRef.MsgNm = truncate(s.OrgnlMsgNmId, 35)
	d.Body.Rpt.ReqHdlg.StsCd = s.TxSts
	d.Body.Rpt.ReqHdlg.Desc = truncate(s.Description, 140)
	return marshal(d, KindAdmi007)
}

func decodeAdmi007(raw []byte) (Status, error) {
	var d docAdmi007
	if err := unmarshal(raw, &d, KindAdmi007); err != nil {
		return Status{}, err
	}
	return Status{Kind: KindAdmi007, MsgId: d.Body.MsgId.MsgId, OrgnlMsgId: d.Body.Rpt.RltdRef.Ref,
		OrgnlMsgNmId: d.Body.Rpt.RltdRef.MsgNm, TxSts: d.Body.Rpt.ReqHdlg.StsCd, Description: d.Body.Rpt.ReqHdlg.Desc}, nil
}
