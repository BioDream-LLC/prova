#!/usr/bin/env python3
# Regenerates testdata/dialects.json: ISO 8583 messages encoded by an independent codec, pyiso8583 4
# (MIT), with specs for prova's Visa-style and Mastercard-style dialects. prova's packer must produce
# the same bytes and its unpacker must read them back. pyiso8583 has no Visa header, so the vectors
# hold the message body; the Go test strips the header prova adds. Run from internal/iso8583:
#
#     pip install pyiso8583 && python3 oracle.py
#
import json, copy, random
import iso8583

def visa_spec():
    n = lambda length: {"data_enc": "b", "len_enc": "b", "len_type": 0, "max_len": (length + 1) // 2, "desc": "", "left_pad": "0"}
    an = lambda length: {"data_enc": "cp037", "len_enc": "b", "len_type": 0, "max_len": length, "desc": ""}
    s = {
        "h": {"data_enc": "b", "len_enc": "b", "len_type": 0, "max_len": 0, "desc": ""},
        "t": {"data_enc": "b", "len_enc": "b", "len_type": 0, "max_len": 2, "desc": ""},
        "p": {"data_enc": "b", "len_enc": "b", "len_type": 0, "max_len": 8, "desc": ""},
        "1": {"data_enc": "b", "len_enc": "b", "len_type": 0, "max_len": 8, "desc": ""},
        "2": {"data_enc": "b", "len_enc": "b", "len_type": 1, "max_len": 19, "desc": "", "len_count": "nibbles", "left_pad": "0"},
        "3": n(6), "4": n(12), "7": n(10), "11": n(6), "14": n(4), "22": n(3), "49": n(3), "70": n(3), "90": n(42),
        "37": an(12), "38": an(6), "39": an(2), "41": an(8),
        "52": {"data_enc": "b", "len_enc": "b", "len_type": 0, "max_len": 8, "desc": ""},
        "55": {"data_enc": "b", "len_enc": "b", "len_type": 2, "max_len": 999, "desc": ""},
        "64": {"data_enc": "b", "len_enc": "b", "len_type": 0, "max_len": 8, "desc": ""},
    }
    return s

def mc_spec():
    n = lambda length: {"data_enc": "cp037", "len_enc": "cp037", "len_type": 0, "max_len": length, "desc": ""}
    s = visa_spec()
    s["t"] = {"data_enc": "cp037", "len_enc": "cp037", "len_type": 0, "max_len": 4, "desc": ""}
    s["2"] = {"data_enc": "cp037", "len_enc": "cp037", "len_type": 2, "max_len": 19, "desc": ""}
    for f, l in [("3", 6), ("4", 12), ("7", 10), ("11", 6), ("14", 4), ("22", 3), ("49", 3), ("70", 3), ("90", 42)]:
        s[f] = n(l)
    s["55"] = {"data_enc": "b", "len_enc": "cp037", "len_type": 3, "max_len": 999, "desc": ""}
    return s

rnd = random.Random(8583)
def digits(k):
    return "".join(rnd.choice("0123456789") for _ in range(k))
def hexs(k):
    return "".join(rnd.choice("0123456789ABCDEF") for _ in range(2 * k))

cases = []
for dialect, spec in [("visa", visa_spec()), ("mastercard", mc_spec())]:
    for i in range(25):
        f = {"t": rnd.choice(["0100", "0110", "0200", "0210", "0400", "0410"]),
             "2": digits(rnd.choice([13, 16, 19])), "3": digits(6), "4": digits(12), "7": digits(10), "11": digits(6),
             "41": rnd.choice(["TERM0001", "ATM 42  ", "POS-77/A"]), "49": rnd.choice(["978", "840", "826"])}
        if rnd.random() < .6: f["14"] = digits(4)
        if rnd.random() < .6: f["22"] = rnd.choice(["051", "071", "010"])
        if rnd.random() < .5: f["39"] = rnd.choice(["00", "51", "55", "N7"])
        if rnd.random() < .4: f["38"] = "A" + digits(5)
        if rnd.random() < .5: f["52"] = hexs(8)
        if rnd.random() < .5: f["55"] = hexs(rnd.randint(8, 120))
        if rnd.random() < .4: f["64"] = hexs(8)
        if rnd.random() < .25: f["90"] = digits(42)
        if rnd.random() < .2: f["70"] = rnd.choice(["001", "301"])
        data = copy.deepcopy(f)
        if dialect == "visa":  # BCD: an odd-length fixed number carries a leading zero nibble
            for k in ("3", "4", "7", "11", "14", "22", "49", "70", "90"):
                if k in data and len(data[k]) % 2:
                    data[k] = "0" + data[k]
        raw, _ = iso8583.encode(data, spec)
        fields = {k: v for k, v in f.items() if k not in ("t",)}
        cases.append({"dialect": dialect, "mti": f["t"], "fields": fields, "hex": raw.hex().upper()})

json.dump(cases, open("testdata/dialects.json", "w"), indent=1)
print(len(cases), "messages")
