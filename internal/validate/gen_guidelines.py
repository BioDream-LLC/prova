#!/usr/bin/env python3
# Regenerates testdata/guidelines/diff.json: libxml2's verdict (through lxml) on the cases
# TestGuidelinesAgreeWithLibxml2 builds for each usage-guideline schema in testdata/guidelines.
#
#     PROVA_DUMP_GUIDELINES=/tmp/gdump go test -run TestGuidelinesAgreeWithLibxml2 ./internal/validate
#     python3 gen_guidelines.py /tmp/gdump        (from internal/validate, with lxml installed)
#
# A schema that declares the Document type but no Document element (TCH's GEFEG exports) gets one,
# as prova does, before libxml2 compiles it.
import glob, json, os, re, sys
import lxml.etree as ET

CASES = ["minimal", "full", "drop", "extra", "enum", "pattern", "maxlen", "date", "decimal",
         "totaldigits", "dup", "space", "attr", "bool", "order", "minlen", "base-minimal", "base-full"]
DUMP = sys.argv[1]
out = {}
for path in sorted(glob.glob("testdata/guidelines/*.xsd")):
    text = open(path, encoding="utf-8").read()
    if not re.search(r'<xs:element\s+name="Document"', text):
        m = re.search(r'xmlns:(\w+)="([^"]+)"', text)
        tns = re.search(r'targetNamespace="([^"]+)"', text).group(1)
        prefix = next(p for p, ns in re.findall(r'xmlns:(\w+)="([^"]+)"', text) if ns == tns)
        text = re.sub(r'(<xs:schema[^>]*>)', r'\1<xs:element name="Document" type="%s:Document"/>' % prefix, text, count=1)
    schema = ET.XMLSchema(ET.fromstring(text.encode("utf-8")))
    name = os.path.basename(path)[:-4]
    v = []
    for c in CASES:
        p = os.path.join(DUMP, name + "__" + c + ".xml")
        if not os.path.exists(p):
            v.append("-")
            continue
        try:
            doc = ET.parse(p)
        except ET.XMLSyntaxError:
            v.append("x")
            continue
        v.append("1" if schema.validate(doc) else "0")
    out[name] = "".join(v)
with open("testdata/guidelines/diff.json", "w") as f:
    json.dump(out, f, indent=1, sort_keys=True)
    f.write("\n")
print(out)
