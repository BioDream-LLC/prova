#!/usr/bin/env python3
# Regenerates testdata_diff.json: the differential schema corpus for TestAgreesWithLibxml2.
#
# For every samples/*-valid.xml it produces the original plus a family of deliberately broken
# mutations, and records libxml2's (via lxml) accept/reject verdict for each. prova's own XSD
# validator must reach the same verdict on every one. Run from internal/validate with lxml installed:
#
#     python3 gen.py
#
import lxml.etree as ET, glob, os, json, re
from collections import Counter


def lxml_valid(xml_bytes):
    try:
        d = ET.fromstring(xml_bytes)
    except Exception:
        return None
    key = d.nsmap.get(None, "").split(":")[-1]
    p = os.path.join("xsd", key + ".xsd")
    if not os.path.exists(p):
        return None
    return bool(ET.XMLSchema(ET.parse(p)).validate(ET.ElementTree(d)))


def mutate(src):
    muts = [("orig", src)]
    for tag in ["NbOfTxs", "TxSts", "CxlId", "ChrgBr", "SttlmMtd", "EndToEndId",
                "MsgId", "CreDtTm", "InitgPty", "PmtMtd", "Conf", "StsReqId", "FinInstnId"]:
        if "<%s>" % tag in src or "<%s " % tag in src:
            muts.append(("drop_%s" % tag, re.sub(r"\s*<%s>.*?</%s>" % (tag, tag), "", src, count=1, flags=re.S)))
    muts += [
        ("bad_sttlmmtd", src.replace("CLRG", "ZZZZ")),
        ("bad_txsts", src.replace("<TxSts>ACSC</TxSts>", "<TxSts>ZZZZ</TxSts>")),
        ("bad_chrgbr", src.replace("<ChrgBr>SHAR</ChrgBr>", "<ChrgBr>WRONG</ChrgBr>")),
        ("bad_pmtmtd", src.replace("<PmtMtd>TRF</PmtMtd>", "<PmtMtd>XX</PmtMtd>")),
        ("bad_conf", src.replace("<Conf>CNCL</Conf>", "<Conf>ZZ</Conf>")),
        ("bad_amt", src.replace("1500.00", "15OO.00").replace("250000.00", "25OO.00")),
        ("amt_frac", src.replace('Ccy="EUR">1500.00', 'Ccy="EUR">1500.123')),
        ("bad_date", re.sub(r"20\d\d-\d\d-\d\dT\d\d:\d\d:\d\dZ", "2026-13-99T09:30:00Z", src, count=1)),
        ("bad_reqdt", src.replace("<Dt>2026-10-20</Dt>", "<Dt>2026-99-99</Dt>")),
        ("bad_bic", src.replace("BANKDEFFXXX", "BANK")),
        ("unexpected", src.replace("</GrpHdr>", "<Bogus>x</Bogus></GrpHdr>")),
        ("bad_iban_pat", src.replace("DE89370400440532013000", "DE")),
    ]
    seen, out = set(), []
    for n, m in muts:
        if m not in seen:
            seen.add(m)
            out.append((n, m))
    return out


cases = []
for f in sorted(glob.glob("samples/*-valid.xml")):
    src = open(f).read()
    for name, m in mutate(src):
        lv = lxml_valid(m.encode())
        if lv is None:
            continue
        cases.append({"base": os.path.basename(f), "mut": name, "xml": m, "lxmlValid": lv})

json.dump(cases, open("testdata_diff.json", "w"))
print(len(cases), "cases", Counter(c["lxmlValid"] for c in cases))
