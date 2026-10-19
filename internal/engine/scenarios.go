package engine

// builtinScenarios are the shipped scenarios. Each names the message it sends, the state the tester has
// seeded (and so knows), and what a compliant host must do. They run against the built-in simulated
// targets (internal/sims), whose seeded accounts, cards, originals and orders they refer to, and against
// any real target set up the same way.
func builtinScenarios() []Scenario {
	var out []Scenario
	out = append(out, iso20022Scenarios()...)
	out = append(out, iso8583Scenarios()...)
	out = append(out, fixScenarios()...)
	return out
}

// with copies m and sets the given key/value pairs.
func with(m map[string]string, kv ...string) map[string]string {
	out := make(map[string]string, len(m)+len(kv)/2)
	for k, v := range m {
		out[k] = v
	}
	for i := 0; i+1 < len(kv); i += 2 {
		out[kv[i]] = kv[i+1]
	}
	return out
}

func kv(pairs ...string) map[string]string { return with(nil, pairs...) }

func iso20022Scenarios() []Scenario {
	mx := func(amt, e2e, acct string) map[string]string {
		return map[string]string{
			"MsgId": "MSG-" + e2e, "EndToEndId": e2e, "Amount": amt, "Currency": "EUR",
			"DebtorAcct": acct, "CreditAcct": "ACC-FR-9", "DebtorBIC": "BANKDEFFXXX", "CreditBIC": "BANKFRPPXXX",
		}
	}
	bal := kv("availableBalance", "142.00")
	ref := func(e2e, orig string) map[string]string {
		return map[string]string{"MsgId": "MSG-" + e2e, "EndToEndId": e2e, "OrgnlEndToEndId": orig,
			"DebtorBIC": "BANKDEFFXXX", "CreditBIC": "BANKFRPPXXX"}
	}
	ret := func(e2e, orig, amt string) map[string]string {
		return with(ref(e2e, orig), "Amount", amt, "Currency", "EUR", "Reason", "AC03")
	}
	p := "iso20022"
	return []Scenario{
		// pacs.008: funds
		{ID: "pacs008-over-balance", Name: "pacs.008 — payment over available balance", Protocol: p,
			Description: "Instruct 150.00 against an account seeded with 142.00. The host must reject with AM04.",
			Request:     mx("150.00", "E2E-OVER", "ACC-EUR-1"), Context: with(bal, "expectReason", "AM04"), Expect: "reject (AM04)"},
		{ID: "pacs008-within-balance", Name: "pacs.008 — payment within balance", Protocol: p,
			Description: "Instruct 90.00 against an account seeded with 142.00. The host must accept.",
			Request:     mx("90.00", "E2E-OK", "ACC-EUR-1"), Context: bal, Expect: "accept (ACSP)"},
		{ID: "pacs008-exact-balance", Name: "pacs.008 — payment equal to balance", Protocol: p,
			Description: "Instruct exactly the available 142.00. The host must accept (boundary case).",
			Request:     mx("142.00", "E2E-EDGE", "ACC-EUR-1"), Context: bal, Expect: "accept (ACSP)"},
		{ID: "pacs008-unknown-balance", Name: "pacs.008 — balance not seeded", Protocol: p,
			Description: "Instruct 150.00 without seeding the balance. prova must return Indeterminate, not guess.",
			Request:     mx("150.00", "E2E-UNK", "ACC-EUR-1"), Context: map[string]string{}, Expect: "indeterminate"},
		{ID: "pacs008-overdraft-unknown", Name: "pacs.008 — over balance, overdraft unknown", Protocol: p,
			Description: "Instruct 150.00 against 142.00 where an overdraft facility may exist. A rejection (AM04) is provably right; an acceptance cannot be called wrong without knowing the overdraft, so prova says Indeterminate.",
			Request:     mx("150.00", "E2E-ODU", "ACC-EUR-1"), Context: with(bal, "overdraftLimit", "unknown", "expectReason", "AM04"), Expect: "reject (AM04); acceptance is indeterminate"},
		// pacs.008: controls
		{ID: "pacs008-currency-mismatch", Name: "pacs.008 — wrong-currency instruction", Protocol: p,
			Description: "Instruct 90.00 USD against a EUR account. With no FX agreed, the host must reject with AM03 (not allowed currency).",
			Request:     with(mx("90.00", "E2E-CCY", "ACC-EUR-1"), "Currency", "USD"), Context: with(bal, "accountCurrency", "EUR", "expectReason", "AM03"), Expect: "reject (AM03)"},
		{ID: "pacs008-duplicate", Name: "pacs.008 — duplicate instruction", Protocol: p,
			Description: "Resubmit an EndToEndId the host already processed. It must reject (AM05), not pay twice.",
			Request:     mx("90.00", "E2E-DUP", "ACC-EUR-1"), Context: with(bal, "duplicate", "true", "expectReason", "AM05"), Expect: "reject (AM05)"},
		{ID: "pacs008-back-valued", Name: "pacs.008 — back-valued instruction", Protocol: p,
			Description: "Instruct with a settlement value date in the past. The host must reject it (DT01) rather than back-value.",
			Request:     with(mx("90.00", "E2E-BVD", "ACC-EUR-1"), "ValueDate", "2020-01-01"), Context: with(bal, "valueDate", "2020-01-01", "settlementDate", "2026-10-15", "expectReason", "DT01"), Expect: "reject (DT01)"},
		{ID: "pacs008-closed-account", Name: "pacs.008 — debit from a closed account", Protocol: p,
			Description: "Instruct 90.00 from an account the host has closed. It must reject with AC04 (closed account number), whatever the balance.",
			Request:     mx("90.00", "E2E-CLOSED", "ACC-EUR-CLOSED"), Context: kv("availableBalance", "1000.00", "accountStatus", "closed", "expectReason", "AC04"), Expect: "reject (AC04)"},
		{ID: "pacs008-blocked-account", Name: "pacs.008 — debit from a blocked account", Protocol: p,
			Description: "Instruct 90.00 from an account the host has blocked. It must reject with AC06 (blocked account).",
			Request:     mx("90.00", "E2E-BLOCKED", "ACC-EUR-BLOCKED"), Context: kv("availableBalance", "1000.00", "accountStatus", "blocked", "expectReason", "AC06"), Expect: "reject (AC06)"},
		{ID: "pacs008-over-limit", Name: "pacs.008 — over the transaction limit", Protocol: p,
			Description: "Instruct 600.00 from an account with ample funds but a 500.00 per-transaction limit. The host must reject with AM02 (not allowed amount).",
			Request:     mx("600.00", "E2E-LIMIT", "ACC-EUR-LTD"), Context: kv("availableBalance", "10000.00", "transactionLimit", "500.00", "expectReason", "AM02"), Expect: "reject (AM02)"},
		{ID: "pacs008-within-limit", Name: "pacs.008 — within the transaction limit", Protocol: p,
			Description: "Instruct 400.00 from the same account, inside its 500.00 limit. The host must accept.",
			Request:     mx("400.00", "E2E-INLIMIT", "ACC-EUR-LTD"), Context: kv("availableBalance", "10000.00", "transactionLimit", "500.00"), Expect: "accept (ACSP)"},
		// pacs.008: field validation
		{ID: "pacs008-zero-amount", Name: "pacs.008 — zero amount", Protocol: p,
			Description: "Instruct 0.00. A payment must move money: the host must reject with AM01 (zero amount).",
			Request:     mx("0.00", "E2E-ZERO", "ACC-EUR-1"), Context: with(bal, "expectReason", "AM01"), Expect: "reject (AM01)"},
		{ID: "pacs008-negative-amount", Name: "pacs.008 — negative amount", Protocol: p,
			Description: "Instruct -10.00. The host must reject with AM12 (invalid amount).",
			Request:     mx("-10.00", "E2E-NEG", "ACC-EUR-1"), Context: with(bal, "expectReason", "AM12"), Expect: "reject (AM12)"},
		{ID: "pacs008-currency-decimals", Name: "pacs.008 — too many decimals for the currency", Protocol: p,
			Description: "Instruct 100.50 JPY. The yen has no minor unit, so the amount is invalid: the host must reject with AM12.",
			Request:     with(mx("100.50", "E2E-JPY", "ACC-EUR-1"), "Currency", "JPY"), Context: with(bal, "expectReason", "AM12"), Expect: "reject (AM12)"},
		{ID: "pacs008-unknown-currency", Name: "pacs.008 — not an ISO 4217 currency", Protocol: p,
			Description: "Instruct 90.00 in XYZ, which is not an ISO 4217 code. The host must reject with AM11 (invalid transaction currency).",
			Request:     with(mx("90.00", "E2E-XYZ", "ACC-EUR-1"), "Currency", "XYZ"), Context: with(bal, "expectReason", "AM11"), Expect: "reject (AM11)"},
		{ID: "pacs008-invalid-bic", Name: "pacs.008 — malformed creditor agent BIC", Protocol: p,
			Description: "Address the creditor agent as BANK-FR-PP, which is not a BIC. The host must reject with RC01 (bank identifier incorrect).",
			Request:     with(mx("90.00", "E2E-BIC", "ACC-EUR-1"), "CreditBIC", "BANK-FR-PP"), Context: with(bal, "expectReason", "RC01"), Expect: "reject (RC01)"},
		{ID: "pacs008-invalid-iban", Name: "pacs.008 — debtor IBAN fails its check digits", Protocol: p,
			Description: "Debit DE00 3704 0044 0532 0130 00, whose check digits are wrong. The host must reject with AC01 (incorrect account number).",
			Request:     with(mx("90.00", "E2E-IBAN", "DE00370400440532013000"), "DebtorIBAN", "DE00370400440532013000"), Context: with(bal, "expectReason", "AC01"), Expect: "reject (AC01)"},
		{ID: "pacs008-valid-iban", Name: "pacs.008 — IBAN to IBAN within balance", Protocol: p,
			Description: "Pay 90.00 from DE89 3704 0044 0532 0130 00 to FR14 2004 1010 0505 0001 3M02 606, both valid IBANs, against a 142.00 balance. The host must accept.",
			Request:     with(mx("90.00", "E2E-IBANOK", "DE89370400440532013000"), "DebtorIBAN", "DE89370400440532013000", "CreditIBAN", "FR1420041010050500013M02606"), Context: bal, Expect: "accept (ACSP)"},
		// pacs.009
		{ID: "pacs009-over-balance", Name: "pacs.009 — bank transfer over available balance", Protocol: p, Message: "pacs.009",
			Description: "A financial-institution credit transfer of 150.00 against 142.00. The host must reject with AM04.",
			Request:     mx("150.00", "E2E-009-OVER", "ACC-EUR-1"), Context: with(bal, "expectReason", "AM04"), Expect: "reject (AM04)"},
		{ID: "pacs009-within-balance", Name: "pacs.009 — bank transfer within balance", Protocol: p, Message: "pacs.009",
			Description: "A financial-institution credit transfer of 90.00 against 142.00. The host must accept.",
			Request:     mx("90.00", "E2E-009-OK", "ACC-EUR-1"), Context: bal, Expect: "accept (ACSP)"},
		{ID: "pacs009-duplicate", Name: "pacs.009 — duplicate bank transfer", Protocol: p, Message: "pacs.009",
			Description: "Resubmit a pacs.009 the host already processed. It must reject with AM05.",
			Request:     mx("90.00", "E2E-DUP-009", "ACC-EUR-1"), Context: with(bal, "duplicate", "true", "expectReason", "AM05"), Expect: "reject (AM05)"},
		// pacs.004 returns
		{ID: "pacs004-return-settled", Name: "pacs.004 — return a settled payment", Protocol: p, Message: "pacs.004",
			Description: "Return the full 90.00 of a payment the host settled. The host must accept the return.",
			Request:     ret("RTR-1", "E2E-ORIG-SETTLED", "90.00"), Context: kv("debit", "false", "originalAmount", "90.00"), Expect: "accept (ACSP)"},
		{ID: "pacs004-return-unknown", Name: "pacs.004 — return of a payment never made", Protocol: p, Message: "pacs.004",
			Description: "Return 90.00 against an EndToEndId the host never processed. It must reject (NOOR, no original transaction).",
			Request:     ret("RTR-2", "E2E-NEVER", "90.00"), Context: kv("debit", "false", "originalKnown", "false", "expectReason", "NOOR"), Expect: "reject (NOOR)"},
		{ID: "pacs004-return-over-original", Name: "pacs.004 — return more than was paid", Protocol: p, Message: "pacs.004",
			Description: "Return 120.00 against a 90.00 payment. The host must reject with AM09 (wrong amount).",
			Request:     ret("RTR-3", "E2E-ORIG-SETTLED", "120.00"), Context: kv("debit", "false", "originalAmount", "90.00", "expectReason", "AM09"), Expect: "reject (AM09)"},
		{ID: "pacs004-return-twice", Name: "pacs.004 — return a payment already returned", Protocol: p, Message: "pacs.004",
			Description: "Return a payment whose funds have already gone back. The host must reject the second return with AM05 (duplication).",
			Request:     ret("RTR-4", "E2E-ORIG-RETURNED", "90.00"), Context: kv("debit", "false", "originalAmount", "90.00", "duplicate", "true", "expectReason", "AM05"), Expect: "reject (AM05)"},
		// camt.056 cancellations
		{ID: "camt056-cancel-pending", Name: "camt.056 — cancel a pending payment", Protocol: p, Message: "camt.056",
			Description: "Ask the host to cancel a payment it has not yet settled. It must cancel it (camt.029 CNCL).",
			Request:     with(ref("CXL-1", "E2E-ORIG-PENDING"), "Reason", "DUPL"), Context: kv("originalPending", "true"), Expect: "cancel (CNCL)"},
		{ID: "camt056-cancel-unknown", Name: "camt.056 — cancel a payment never made", Protocol: p, Message: "camt.056",
			Description: "Ask the host to cancel an EndToEndId it never processed. It must refuse (RJCR, NOOR).",
			Request:     with(ref("CXL-2", "E2E-NEVER"), "Reason", "DUPL"), Context: kv("originalKnown", "false", "expectReason", "NOOR"), Expect: "refuse (RJCR / NOOR)"},
		{ID: "camt056-cancel-returned", Name: "camt.056 — cancel a payment already returned", Protocol: p, Message: "camt.056",
			Description: "Ask the host to cancel a payment whose funds have already been returned. It must refuse (RJCR, ARDT).",
			Request:     with(ref("CXL-3", "E2E-ORIG-RETURNED"), "Reason", "DUPL"), Context: kv("originalFinal", "true", "expectReason", "ARDT"), Expect: "refuse (RJCR / ARDT)"},
		{ID: "camt056-cancel-settled", Name: "camt.056 — cancel a settled payment", Protocol: p, Message: "camt.056",
			Description: "Ask the host to cancel a payment it has settled. Only the creditor can agree to give the money back, so the host must refuse (RJCR, CUST).",
			Request:     with(ref("CXL-4", "E2E-ORIG-SETTLED"), "Reason", "DUPL"), Context: kv("originalFinal", "true", "expectReason", "CUST"), Expect: "refuse (RJCR / CUST)"},
		// pacs.028 status requests
		{ID: "pacs028-status-settled", Name: "pacs.028 — status of a settled payment", Protocol: p, Message: "pacs.028",
			Description: "Ask for the status of a payment the host settled. Its pacs.002 must say ACSC.",
			Request:     ref("STS-1", "E2E-ORIG-SETTLED"), Context: kv("originalStatus", "ACSC"), Expect: "report ACSC"},
		{ID: "pacs028-status-pending", Name: "pacs.028 — status of a pending payment", Protocol: p, Message: "pacs.028",
			Description: "Ask for the status of a payment still pending. Its pacs.002 must say PDNG, not that the money has arrived.",
			Request:     ref("STS-2", "E2E-ORIG-PENDING"), Context: kv("originalStatus", "PDNG"), Expect: "report PDNG"},
		{ID: "pacs028-status-unknown", Name: "pacs.028 — status of a payment never made", Protocol: p, Message: "pacs.028",
			Description: "Ask for the status of an EndToEndId the host never processed. It must reject (NOOR), not invent a status.",
			Request:     ref("STS-3", "E2E-NEVER"), Context: kv("originalKnown", "false", "expectReason", "NOOR"), Expect: "reject (NOOR)"},
		// pain.001 customer credit transfer initiation
		{ID: "pain001-within-balance", Name: "pain.001 — customer transfer within balance", Protocol: p, Message: "pain.001",
			Description: "A customer initiates a 90.00 credit transfer from an account seeded with 142.00. The host must accept (pain.002 ACSP).",
			Request:     mx("90.00", "E2E-PAIN-OK", "ACC-EUR-1"), Context: bal, Expect: "accept (ACSP)"},
		{ID: "pain001-over-balance", Name: "pain.001 — customer transfer over balance", Protocol: p, Message: "pain.001",
			Description: "A customer initiates a 150.00 transfer against 142.00. The host must reject the initiation (pain.002 RJCT, AM04).",
			Request:     mx("150.00", "E2E-PAIN-OVER", "ACC-EUR-1"), Context: with(bal, "expectReason", "AM04"), Expect: "reject (AM04)"},
		{ID: "pain001-unknown-currency", Name: "pain.001 — initiation in an unknown currency", Protocol: p, Message: "pain.001",
			Description: "A customer initiates a transfer in XYZ, not an ISO 4217 code. The host must reject with AM11.",
			Request:     with(mx("90.00", "E2E-PAIN-XYZ", "ACC-EUR-1"), "Currency", "XYZ"), Context: with(bal, "expectReason", "AM11"), Expect: "reject (AM11)"},
	}
}

func iso8583Scenarios() []Scenario {
	card := func(amt, stan string) map[string]string {
		return map[string]string{
			"PAN": "4111111111111111", "ProcessingCode": "000000", "Amount": amt, "Currency": "EUR",
			"STAN": stan, "Terminal": "TERM0001", "Expiry": "3012",
		}
	}
	bal := kv("availableBalance", "142.00")
	p := "iso8583"
	return []Scenario{
		{ID: "iso8583-over-limit", Name: "ISO 8583 0100 — authorisation over balance", Protocol: p,
			Description: "Authorise 150.00 on a card seeded with 142.00. The switch must decline with DE39=51.",
			Request:     card("150.00", "000101"), Context: with(bal, "expectReason", "51"), Expect: "decline (DE39=51)"},
		{ID: "iso8583-within-limit", Name: "ISO 8583 0100 — authorisation within balance", Protocol: p,
			Description: "Authorise 90.00 on a card seeded with 142.00. The switch must approve (DE39=00).",
			Request:     card("90.00", "000102"), Context: bal, Expect: "approve (DE39=00)"},
		{ID: "iso8583-exact-balance", Name: "ISO 8583 0100 — authorisation equal to balance", Protocol: p,
			Description: "Authorise exactly the available 142.00. The switch must approve (boundary case).",
			Request:     card("142.00", "000103"), Context: bal, Expect: "approve (DE39=00)"},
		{ID: "iso8583-unknown-balance", Name: "ISO 8583 0100 — balance not seeded", Protocol: p,
			Description: "Authorise 150.00 without seeding the balance. Whatever the switch answers, prova cannot know it was right, and says so.",
			Request:     card("150.00", "000104"), Context: map[string]string{}, Expect: "indeterminate"},
		{ID: "iso8583-invalid-pan", Name: "ISO 8583 0100 — card number fails the Luhn check", Protocol: p,
			Description: "Authorise 90.00 on 4111 1111 1111 1112, which fails its check digit. The switch must decline with DE39=14 (invalid card number).",
			Request:     with(card("90.00", "000105"), "PAN", "4111111111111112"), Context: with(bal, "expectReason", "14"), Expect: "decline (DE39=14)"},
		{ID: "iso8583-expired-card", Name: "ISO 8583 0100 — expired card", Protocol: p,
			Description: "Authorise 90.00 on a card that expired in January 2024. The switch must decline with DE39=54 (expired card).",
			Request:     with(card("90.00", "000106"), "Expiry", "2401"), Context: with(bal, "processingDate", "2026-10-15", "expectReason", "54"), Expect: "decline (DE39=54)"},
		{ID: "iso8583-lost-card", Name: "ISO 8583 0100 — card reported lost", Protocol: p,
			Description: "Authorise 90.00 on a card reported lost. The switch must decline with DE39=41 (lost card, pick up).",
			Request:     with(card("90.00", "000107"), "PAN", "4000000000000002"), Context: kv("availableBalance", "1000.00", "accountStatus", "lost", "expectReason", "41"), Expect: "decline (DE39=41)"},
		{ID: "iso8583-stolen-card", Name: "ISO 8583 0100 — card reported stolen", Protocol: p,
			Description: "Authorise 90.00 on a card reported stolen. The switch must decline with DE39=43 (stolen card, pick up).",
			Request:     with(card("90.00", "000108"), "PAN", "4000000000000010"), Context: kv("availableBalance", "1000.00", "accountStatus", "stolen", "expectReason", "43"), Expect: "decline (DE39=43)"},
		{ID: "iso8583-duplicate", Name: "ISO 8583 0100 — duplicate transmission", Protocol: p,
			Description: "Resend an authorisation whose STAN the switch has already processed. It must decline with DE39=94 (duplicate transmission).",
			Request:     card("90.00", "000150"), Context: with(bal, "duplicate", "true", "expectReason", "94"), Expect: "decline (DE39=94)"},
		{ID: "iso8583-zero-amount", Name: "ISO 8583 0100 — zero amount", Protocol: p,
			Description: "Authorise 0.00 as a purchase. The switch must decline with DE39=13 (invalid amount).",
			Request:     card("0.00", "000109"), Context: with(bal, "expectReason", "13"), Expect: "decline (DE39=13)"},
		{ID: "iso8583-over-card-limit", Name: "ISO 8583 0100 — over the card's transaction limit", Protocol: p,
			Description: "Authorise 600.00 on a card with ample funds but a 500.00 per-transaction limit. The switch must decline with DE39=61 (exceeds amount limit).",
			Request:     with(card("600.00", "000110"), "PAN", "5555555555554444"), Context: kv("availableBalance", "1000.00", "transactionLimit", "500.00", "expectReason", "61"), Expect: "decline (DE39=61)"},
		{ID: "iso8583-0200-over-balance", Name: "ISO 8583 0200 — purchase over balance", Protocol: p, Message: "0200",
			Description: "A financial transaction (single-message purchase) of 150.00 against 142.00. The switch must decline with DE39=51.",
			Request:     card("150.00", "000111"), Context: with(bal, "expectReason", "51"), Expect: "decline (DE39=51)"},
		{ID: "iso8583-0200-within-balance", Name: "ISO 8583 0200 — purchase within balance", Protocol: p, Message: "0200",
			Description: "A financial transaction of 90.00 against 142.00. The switch must approve.",
			Request:     card("90.00", "000112"), Context: bal, Expect: "approve (DE39=00)"},
		{ID: "iso8583-reversal-known", Name: "ISO 8583 0400 — reverse an approved purchase", Protocol: p, Message: "0400",
			Description: "Reverse the full 90.00 of a purchase the switch approved (STAN 000201). It must approve the reversal.",
			Request:     with(card("90.00", "000113"), "OriginalSTAN", "000201"), Context: kv("debit", "false", "originalAmount", "90.00"), Expect: "approve (DE39=00)"},
		{ID: "iso8583-reversal-unknown", Name: "ISO 8583 0400 — reverse a purchase never made", Protocol: p, Message: "0400",
			Description: "Reverse STAN 000999, which the switch never approved. It must decline with DE39=25 (unable to locate the original).",
			Request:     with(card("90.00", "000114"), "OriginalSTAN", "000999"), Context: kv("debit", "false", "originalKnown", "false", "expectReason", "25"), Expect: "decline (DE39=25)"},
		{ID: "iso8583-reversal-over-original", Name: "ISO 8583 0400 — reverse more than was approved", Protocol: p, Message: "0400",
			Description: "Reverse 120.00 against a 90.00 approval. The switch must decline with DE39=13 (invalid amount).",
			Request:     with(card("120.00", "000115"), "OriginalSTAN", "000201"), Context: kv("debit", "false", "originalAmount", "90.00", "expectReason", "13"), Expect: "decline (DE39=13)"},
		{ID: "iso8583-echo", Name: "ISO 8583 0800 — network echo test", Protocol: p, Message: "0800",
			Description: "Send a network-management echo test (DE70=301). The switch must answer 0810 with DE39=00.",
			Request:     kv("STAN", "000116", "NetworkCode", "301"), Context: map[string]string{}, Expect: "0810 (DE39=00)"},
		// Card security: PIN block (DE52), MAC (DE64) and EMV chip data (DE55)
		{ID: "iso8583-chip-valid", Name: "ISO 8583 0100 — chip purchase with ARQC", Protocol: p,
			Description: "A chip-read (DE22=051) authorisation carrying a valid PIN block, MAC and EMV data with an Application Cryptogram (9F26). Within balance, the switch must approve.",
			Request: with(card("90.00", "000120"), "POSEntryMode", "051", "PINBlock", "0412FFFFFFFFFF99",
				"MAC", "A1B2C3D4E5F60718", "ICCData", "9F26080123456789ABCDEF9F2701809F3602000195050000000000"),
			Context: bal, Expect: "approve (DE39=00)"},
		{ID: "iso8583-chip-no-emv", Name: "ISO 8583 0100 — chip entry without EMV data", Protocol: p,
			Description: "A chip-read entry (DE22=051) carrying no DE55 EMV data at all. The switch must decline with DE39=30 (format error).",
			Request:     with(card("90.00", "000121"), "POSEntryMode", "051"), Context: with(bal, "expectReason", "30"), Expect: "decline (DE39=30)"},
		{ID: "iso8583-chip-no-arqc", Name: "ISO 8583 0100 — EMV data missing its cryptogram", Protocol: p,
			Description: "A chip authorisation whose DE55 is well-formed TLV but carries no Application Cryptogram (9F26). The switch must decline with DE39=30.",
			Request:     with(card("90.00", "000122"), "POSEntryMode", "051", "ICCData", "9F2701809F3602000195050000000000"), Context: with(bal, "expectReason", "30"), Expect: "decline (DE39=30)"},
		{ID: "iso8583-bad-pin-block", Name: "ISO 8583 0100 — malformed PIN block", Protocol: p,
			Description: "An authorisation whose DE52 PIN block is not a 16-hex ISO 9564 block. The switch must decline with DE39=30.",
			Request:     with(card("90.00", "000123"), "PINBlock", "0412FFFF"), Context: with(bal, "expectReason", "30"), Expect: "decline (DE39=30)"},
		{ID: "iso8583-bad-mac", Name: "ISO 8583 0100 — malformed MAC", Protocol: p,
			Description: "An authorisation whose DE64 MAC is not 16 or 32 hex characters. The switch must decline with DE39=30.",
			Request:     with(card("90.00", "000124"), "MAC", "A1B2C3"), Context: with(bal, "expectReason", "30"), Expect: "decline (DE39=30)"},
	}
}

func fixScenarios() []Scenario {
	order := func(amt, qty, clord string) map[string]string {
		return map[string]string{
			"Account": "ACC1", "Symbol": "ACME", "Side": "buy", "OrderQty": qty, "Price": "10.00",
			"Currency": "USD", "Amount": amt, "ClOrdID": clord,
		}
	}
	bp := kv("availableBalance", "142.00")
	about := func(clord, orig string) map[string]string {
		return map[string]string{"ClOrdID": clord, "OrigClOrdID": orig, "Account": "ACC1", "Symbol": "ACME", "Side": "buy", "OrderQty": "10"}
	}
	p := "fix"
	return []Scenario{
		{ID: "fix-over-limit", Name: "FIX D — order over buying power", Protocol: p,
			Description: "Send a 15 x 10.00 (150.00) order on an account with 142.00 buying power. The venue must reject with OrdRejReason=3.",
			Request:     order("150.00", "15", "ORD-OVER"), Context: with(bp, "expectReason", "3"), Expect: "reject (OrdRejReason=3)"},
		{ID: "fix-within-limit", Name: "FIX D — order within buying power", Protocol: p,
			Description: "Send a 9 x 10.00 (90.00) order on an account with 142.00 buying power. The venue must fill (OrdStatus=2).",
			Request:     order("90.00", "9", "ORD-OK"), Context: bp, Expect: "fill (OrdStatus=2)"},
		{ID: "fix-exact-limit", Name: "FIX D — order equal to buying power", Protocol: p,
			Description: "Send a 10 x 14.20 (142.00) order on an account with exactly 142.00 buying power. The venue must fill (boundary case).",
			Request:     with(order("142.00", "10", "ORD-EDGE"), "Price", "14.20"), Context: bp, Expect: "fill (OrdStatus=2)"},
		{ID: "fix-unknown-symbol", Name: "FIX D — order for an unlisted instrument", Protocol: p,
			Description: "Send an order for ZZZZ, which the venue does not list. It must reject with OrdRejReason=1 (unknown symbol).",
			Request:     with(order("90.00", "9", "ORD-SYM"), "Symbol", "ZZZZ"), Context: with(bp, "instrumentKnown", "false", "expectReason", "1"), Expect: "reject (OrdRejReason=1)"},
		{ID: "fix-market-closed", Name: "FIX D — order while the market is closed", Protocol: p,
			Description: "Send an order for GLOBX while its market is halted. The venue must reject with OrdRejReason=2 (exchange closed).",
			Request:     with(order("90.00", "9", "ORD-HALT"), "Symbol", "GLOBX"), Context: with(bp, "marketOpen", "false", "expectReason", "2"), Expect: "reject (OrdRejReason=2)"},
		{ID: "fix-zero-quantity", Name: "FIX D — order for zero shares", Protocol: p,
			Description: "Send an order with OrderQty=0. The venue must reject with OrdRejReason=13 (incorrect quantity).",
			Request:     order("0.00", "0", "ORD-ZERO"), Context: with(bp, "expectReason", "13"), Expect: "reject (OrdRejReason=13)"},
		{ID: "fix-duplicate-clordid", Name: "FIX D — order reusing a ClOrdID", Protocol: p,
			Description: "Send a new order with a ClOrdID the venue has already seen. It must reject with OrdRejReason=6 (duplicate order).",
			Request:     order("90.00", "9", "ORD-USED"), Context: with(bp, "duplicate", "true", "expectReason", "6"), Expect: "reject (OrdRejReason=6)"},
		{ID: "fix-sell-within-position", Name: "FIX D — sell within the position held", Protocol: p,
			Description: "Sell 15 ACME from a position of 20. The venue must fill.",
			Request:     kv("Account", "ACC1", "Symbol", "ACME", "Side", "sell", "OrderQty", "15", "Price", "10.00", "Currency", "USD", "ClOrdID", "ORD-SELL"),
			Context:     kv("position", "20"), Expect: "fill (OrdStatus=2)"},
		{ID: "fix-sell-over-position", Name: "FIX D — sell more than the position held", Protocol: p,
			Description: "Sell 25 ACME from a position of 20, with no short-selling facility. The venue must reject with OrdRejReason=3.",
			Request:     kv("Account", "ACC1", "Symbol", "ACME", "Side", "sell", "OrderQty", "25", "Price", "10.00", "Currency", "USD", "ClOrdID", "ORD-SHORT"),
			Context:     kv("position", "20", "expectReason", "3"), Expect: "reject (OrdRejReason=3)"},
		{ID: "fix-cancel-open", Name: "FIX F — cancel an open order", Protocol: p, Message: "F",
			Description: "Cancel ORD-OPEN, which is working. The venue must confirm with an ExecutionReport (ExecType=4).",
			Request:     about("CXL-OPEN", "ORD-OPEN"), Context: kv("originalPending", "true"), Expect: "cancelled (ExecType=4)"},
		{ID: "fix-cancel-filled", Name: "FIX F — cancel a filled order", Protocol: p, Message: "F",
			Description: "Cancel ORD-FILLED, which has already filled. The venue must refuse with an OrderCancelReject (CxlRejReason=0, too late to cancel).",
			Request:     about("CXL-FILLED", "ORD-FILLED"), Context: kv("originalFinal", "true", "expectReason", "0"), Expect: "refuse (35=9, CxlRejReason=0)"},
		{ID: "fix-cancel-unknown", Name: "FIX F — cancel an unknown order", Protocol: p, Message: "F",
			Description: "Cancel ORD-NONE, which the venue never received. It must refuse with CxlRejReason=1 (unknown order).",
			Request:     about("CXL-NONE", "ORD-NONE"), Context: kv("originalKnown", "false", "expectReason", "1"), Expect: "refuse (35=9, CxlRejReason=1)"},
		{ID: "fix-replace-within", Name: "FIX G — replace within buying power", Protocol: p, Message: "G",
			Description: "Replace ORD-OPEN with 9 x 10.00 (90.00), within 142.00 buying power. The venue must confirm (ExecType=5).",
			Request:     with(about("RPL-OK", "ORD-OPEN"), "OrderQty", "9", "Price", "10.00", "Currency", "USD", "Amount", "90.00"), Context: bp, Expect: "replaced (ExecType=5)"},
		{ID: "fix-replace-over", Name: "FIX G — replace over buying power", Protocol: p, Message: "G",
			Description: "Replace ORD-OPEN with 15 x 10.00 (150.00), over 142.00 buying power. The venue must refuse with CxlRejReason=2.",
			Request:     with(about("RPL-OVER", "ORD-OPEN"), "OrderQty", "15", "Price", "10.00", "Currency", "USD", "Amount", "150.00"), Context: with(bp, "expectReason", "2"), Expect: "refuse (35=9, CxlRejReason=2)"},
		{ID: "fix-status-open", Name: "FIX H — status of an open order", Protocol: p, Message: "H",
			Description: "Ask for the status of ORD-OPEN. The venue must report it New (OrdStatus=0), not filled.",
			Request:     kv("OrigClOrdID", "ORD-OPEN", "Symbol", "ACME", "Side", "buy"), Context: kv("originalStatus", "0"), Expect: "report OrdStatus=0"},
		{ID: "fix-status-unknown", Name: "FIX H — status of an unknown order", Protocol: p, Message: "H",
			Description: "Ask for the status of ORD-NONE, which the venue never received. It must reject (OrdRejReason=5, unknown order).",
			Request:     kv("OrigClOrdID", "ORD-NONE", "Symbol", "ACME", "Side", "buy"), Context: kv("originalKnown", "false", "expectReason", "5"), Expect: "reject (OrdRejReason=5)"},
		{ID: "fix-partial-fill", Name: "FIX D — order partially filled", Protocol: p,
			Description: "Send a 9 x 10.00 order for PARTL, which the venue only partly fills (ExecType=1, OrdStatus=1), leaving the balance working. Within buying power, the venue must accept and begin filling.",
			Request:     kv("Account", "ACC1", "Symbol", "PARTL", "Side", "buy", "OrderQty", "9", "Price", "10.00", "Currency", "USD", "Amount", "90.00"),
			Context:     kv("availableBalance", "142.00"), Expect: "partial fill (ExecType=1, OrdStatus=1)"},
	}
}
