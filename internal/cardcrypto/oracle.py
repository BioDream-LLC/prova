#!/usr/bin/env python3
# Regenerates testdata/vectors.json: card-security test vectors computed by independent
# implementations, which prova's cardcrypto package must reproduce.
#
#   psec 1.3 (MIT)      PIN blocks (ISO 9564 formats 0, 3, 4), retail and CBC MACs
#   pyemv 1.5 (MIT)     EMV master and session keys, Visa CVN 10/18 and Mastercard CVN 16 cryptograms
#   psec also           the Visa PVV and CVV
#   cryptography        AES-CMAC
#
# None of them ships with prova; they are test oracles only. Run from internal/cardcrypto:
#
#     pip install psec pyemv pynblock cryptography
#     python3 oracle.py
#
import json, os, random
from binascii import hexlify, unhexlify
from psec import pinblock, mac as psmac, des as psdes
from pyemv import cvn
from psec import cvv as pscvv, pin as pspin
from cryptography.hazmat.primitives import cmac
from cryptography.hazmat.primitives.ciphers import algorithms

rnd = random.Random(20261030)


def rhex(n):
    return "".join(rnd.choice("0123456789ABCDEF") for _ in range(2 * n))


def luhn_pan(prefix, length):
    digits = prefix + "".join(rnd.choice("0123456789") for _ in range(length - len(prefix) - 1))
    s = 0
    for i, c in enumerate(reversed(digits)):
        d = int(c)
        if i % 2 == 0:
            d *= 2
            if d > 9:
                d -= 9
        s += d
    return digits + str((10 - s % 10) % 10)


def H(b):
    return hexlify(b).decode().upper()


out = {"pin": [], "mac": [], "arqc": [], "pvv": [], "cvv": [], "cmac": []}

for i in range(40):
    pan = luhn_pan(rnd.choice(["4", "5", "6011"]), rnd.choice([16, 16, 19, 13]))
    pin = "".join(rnd.choice("0123456789") for _ in range(rnd.choice([4, 4, 5, 6, 12])))
    zpk = unhexlify(rhex(16))
    fmt = [0, 3, 4][i % 3]
    if fmt == 4:
        key = unhexlify(rhex(rnd.choice([16, 24, 32])))
        block = pinblock.encipher_pinblock_iso_4(key, pin, pan)
    else:
        key = zpk
        clear = pinblock.encode_pinblock_iso_0(pin, pan) if fmt == 0 else pinblock.encode_pinblock_iso_3(pin, pan)
        block = psdes.encrypt_tdes_ecb(key, clear)
    out["pin"].append({"format": fmt, "key": H(key), "pin": pin, "pan": pan, "block": H(block)})

for i in range(30):
    key = unhexlify(rhex(16))
    data = unhexlify(rhex(rnd.choice([0, 1, 7, 8, 9, 16, 33])))
    if len(data) == 0:
        data = b"\x01"
    out["mac"].append({
        "key": H(key), "data": H(data),
        "retail": H(psmac.generate_retail_mac(key[:8], key[8:], data, 1)),
        "cbc": H(psmac.generate_cbc_mac(key, data, 1)),
    })

for i in range(20):
    key = unhexlify(rhex(rnd.choice([16, 24, 32])))
    data = unhexlify(rhex(rnd.choice([0, 1, 15, 16, 17, 32, 40])))
    c = cmac.CMAC(algorithms.AES(key))
    c.update(data)
    out["cmac"].append({"key": H(key), "data": H(data), "mac": H(c.finalize())})

for i in range(30):
    pan = luhn_pan(rnd.choice(["4", "5"]), rnd.choice([16, 16, 18, 19]))
    psn = "%02d" % rnd.randint(0, 3)
    imk = unhexlify(rhex(16))
    smi, smc = unhexlify(rhex(16)), unhexlify(rhex(16))
    t = {
        "9F02": "%012d" % rnd.randint(1, 999999), "9F03": "000000000000", "9F1A": rnd.choice(["0276", "0840", "0250"]),
        "95": rhex(5), "5F2A": rnd.choice(["0978", "0840"]), "9A": "2610%02d" % rnd.randint(1, 28), "9C": "00",
        "9F37": rhex(4), "82": rnd.choice(["5800", "3900", "1980"]), "9F36": rhex(2),
    }
    b = {k: unhexlify(v) for k, v in t.items()}
    version = ["visa-cvn10", "visa-cvn18", "mc-cvn16"][i % 3]
    if version == "visa-cvn10":
        iad = "06010A03" + rhex(3)
        card = cvn.VisaCVN10(imk, smi, smc, pan, psn)
        ac = card.generate_ac(b["9F02"], b["9F03"], b["9F1A"], b["95"], b["5F2A"], b["9A"], b["9C"], b["9F37"], b["82"], b["9F36"], unhexlify(iad)[3:7])
    elif version == "visa-cvn18":
        iad = "0601120" + "3" + rhex(3)
        card = cvn.VisaCVN18(imk, smi, smc, pan, psn)
        ac = card.generate_ac(b["9F02"], b["9F03"], b["9F1A"], b["95"], b["5F2A"], b["9A"], b["9C"], b["9F37"], b["82"], b["9F36"], unhexlify(iad))
    else:
        iad = "0110" + rhex(6) + "0000000000000000000000FF"
        card = cvn.MasterCardCVN16(imk, smi, smc, pan, psn)
        ac = card.generate_ac(b["9F02"], b["9F03"], b["9F1A"], b["95"], b["5F2A"], b["9A"], b["9C"], b["9F37"], b["82"], b["9F36"], unhexlify(iad)[2:8])
    t["9F10"] = iad
    out["arqc"].append({"version": version, "imk": H(imk), "pan": pan, "psn": psn, "tags": t, "ac": H(ac)})

for i in range(20):
    pan = luhn_pan("4", 16)
    pvk = rhex(16)
    pin = "".join(rnd.choice("0123456789") for _ in range(4))
    pvki = str(rnd.randint(0, 6))
    pvv = pspin.generate_visa_pvv(unhexlify(pvk), pvki, pin, pan)
    out["pvv"].append({"pvk": pvk, "pan": pan, "pvki": pvki, "pin": pin, "pvv": pvv})
    cvk = rhex(16)
    exp = "%02d%02d" % (rnd.randint(26, 35), rnd.randint(1, 12))
    sc = rnd.choice(["101", "201", "000", "999"])
    out["cvv"].append({"cvk": cvk, "pan": pan, "expiry": exp, "service": sc,
                       "cvv": pscvv.generate_cvv(unhexlify(cvk), pan, exp, sc)})

os.makedirs("testdata", exist_ok=True)
json.dump(out, open("testdata/vectors.json", "w"), indent=1)
print({k: len(v) for k, v in out.items()})
