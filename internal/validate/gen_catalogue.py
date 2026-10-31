#!/usr/bin/env python3
# Regenerates catalogue/diff.json: libxml2's verdict (through lxml) on every case of every schema in
# the ISO 20022 catalogue, for TestCatalogueAgreesWithLibxml2. First write the cases, then judge them:
#
#     PROVA_DUMP_CATALOGUE=/tmp/catdump go test -run TestCatalogueAgreesWithLibxml2 ./internal/validate
#     python3 gen_catalogue.py /tmp/catdump        (from internal/validate, with lxml installed)
#
# Each value is one character per case, in the order of catalogueCases in catalogue_diff_test.go:
# 1 valid, 0 invalid, - no such case for that schema.
import json, os, sys, zipfile
from multiprocessing import Pool
import lxml.etree as ET

CASES = ["minimal", "full", "drop", "extra", "enum", "pattern", "maxlen", "date", "decimal",
         "totaldigits", "dup", "space", "attr", "bool", "order", "minlen"]
DUMP = sys.argv[1]
ZIP = os.path.join("catalogue", "iso20022-schemas.zip")


def judge(kind):
    with zipfile.ZipFile(ZIP) as z:
        schema = ET.XMLSchema(ET.fromstring(z.read(kind + ".xsd")))
    out = []
    for c in CASES:
        p = os.path.join(DUMP, kind + "__" + c + ".xml")
        if not os.path.exists(p):
            out.append("-")
            continue
        try:
            doc = ET.parse(p)
        except ET.XMLSyntaxError:
            out.append("x")
            continue
        out.append("1" if schema.validate(doc) else "0")
    return kind, "".join(out)


if __name__ == "__main__":
    with zipfile.ZipFile(ZIP) as z:
        kinds = sorted(n[:-4] for n in z.namelist() if n.endswith(".xsd"))
    with Pool() as pool:
        res = dict(pool.map(judge, kinds, chunksize=8))
    with open(os.path.join("catalogue", "diff.json"), "w") as f:
        json.dump(res, f, indent=0, sort_keys=True)
        f.write("\n")
    n = sum(len(v) - v.count("-") for v in res.values())
    print(len(res), "schemas,", n, "cases")
