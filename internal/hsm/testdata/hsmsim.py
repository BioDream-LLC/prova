#!/usr/bin/env python3
# Test harness for prova's HSM client: runs the pythales payShield simulator (LGPL, not shipped with
# prova) on a port, or computes what it expects.
#
#   hsmsim.py serve PORT HEADER     run the simulator
#   hsmsim.py pvv ACCT12 PVKI PIN PVK   print the PVV pythales computes for an EC command
#
# pythales' CVV routine does not run on current pycryptodome; it is replaced here by psec's, applied to
# the key text the simulator passes it (pythales does not decrypt the CVK under its LMK).
import sys
from binascii import unhexlify
import pynblock.tools as t
from psec import cvv as pscvv


def cvv(account_number, exp_date, service_code, CVK):
    return pscvv.generate_cvv(unhexlify(CVK[:32]), account_number.decode(), exp_date.decode(), service_code.decode())


t.get_visa_cvv = cvv
import pythales.hsm as h
h.get_visa_cvv = cvv
h.trace = lambda *a, **k: None  # the published tracetools no longer matches pythales' call

if sys.argv[1] == "serve":
    h.HSM(header=sys.argv[3] or None, port=int(sys.argv[2])).run()
elif sys.argv[1] == "pvv":
    print(t.get_visa_pvv(sys.argv[2].encode(), sys.argv[3].encode(), sys.argv[4].encode(), sys.argv[5].encode()).decode())
