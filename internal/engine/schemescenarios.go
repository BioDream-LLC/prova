package engine

// schemeScenarios run against the scheme hosts: a SEPA host that screens every message against the
// EPC rules, and a FedNow participant that screens against FedNow's.
func schemeScenarios() []Scenario {
	p := "iso20022"
	sepa := func(e2e, amount, ccy string) map[string]string {
		return map[string]string{
			"MsgId": "MSG-" + e2e, "EndToEndId": e2e, "Amount": amount, "Currency": ccy, "ServiceLevel": "SEPA",
			"DebtorIBAN": "DE89370400440532013000", "DebtorAcct": "DE89370400440532013000", "CreditIBAN": "FR1420041010050500013M02606",
			"DebtorBIC": "COBADEFFXXX", "CreditBIC": "BNPAFRPPXXX", "DebtorName": "Muster GmbH", "CreditorName": "Exemple SARL",
			"Remittance": "Invoice 2026-0042",
		}
	}
	fed := func(e2e, amount, ccy string) map[string]string {
		return map[string]string{
			"MsgId": "MSG-" + e2e, "EndToEndId": e2e, "Amount": amount, "Currency": ccy,
			"DebtorRTN": "011000015", "CreditRTN": "021000021", "DebtorAcct": "1234567890", "CreditAcct": "9876543210",
			"DebtorName": "Acme Corp", "CreditorName": "Widget LLC",
		}
	}
	big := kv("availableBalance", "2000000000.00")
	usd := kv("availableBalance", "5000000.00")
	sc := func(target, id, name, desc string, req, ctx map[string]string, expect string) Scenario {
		return Scenario{ID: id, Name: name, Protocol: p, Description: desc, Request: req, Context: ctx, Expect: expect, Target: target}
	}
	return []Scenario{
		sc("sepa-host", "sepa-credit-transfer", "SEPA SCT — a compliant credit transfer",
			"A 250.00 EUR SEPA credit transfer between two IBANs, service level SEPA, charges SLEV, EPC character set. The SEPA host must accept it.",
			sepa("SEPA-OK-1", "250.00", "EUR"), big, "accept (ACSP)"),
		sc("sepa-host", "sepa-inst-credit-transfer", "SEPA SCT Inst — a compliant instant transfer",
			"The same transfer as an instant payment (local instrument INST). The SEPA host must accept it.",
			with(sepa("SEPA-INST-1", "250.00", "EUR"), "LocalInstrument", "INST"), big, "accept (ACSP)"),
		sc("sepa-host", "sepa-not-euro", "SEPA SCT — a credit transfer in US dollars",
			"A SEPA credit transfer must be in euro. The host must refuse a USD instruction with AM03 (not allowed currency).",
			sepa("SEPA-USD-1", "250.00", "USD"), with(big, "accountCurrency", "EUR", "expectReason", "AM03"), "reject (AM03)"),
		sc("sepa-host", "sepa-over-maximum", "SEPA SCT — above the scheme's maximum amount",
			"A transfer of 1,000,000,000.00 EUR, one cent above the scheme's 999,999,999.99 maximum, from an account that could cover it. The host must refuse it with AM02 (not allowed amount).",
			sepa("SEPA-MAX-1", "1000000000.00", "EUR"), with(big, "transactionLimit", "999999999.99", "expectReason", "AM02"), "reject (AM02)"),
		sc("sepa-host", "sepa-at-maximum", "SEPA SCT — exactly the scheme's maximum amount",
			"A transfer of exactly 999,999,999.99 EUR, the scheme's maximum. The host must accept it (boundary case).",
			sepa("SEPA-MAX-2", "999999999.99", "EUR"), with(big, "transactionLimit", "999999999.99"), "accept (ACSP)"),
		sc("sepa-host", "sepa-bad-iban", "SEPA SCT — creditor IBAN fails its check digits",
			"A transfer to FR00 2004 1010 0505 0001 3M02 606, whose check digits are wrong. The host must refuse it with AC01.",
			with(sepa("SEPA-IBAN-1", "250.00", "EUR"), "CreditIBAN", "FR0020041010050500013M02606"), with(big, "expectReason", "AC01"), "reject (AC01)"),
		sc("fednow-host", "fednow-credit-transfer", "FedNow — a compliant customer credit transfer",
			"A 2,500.00 USD transfer between two US banks identified by routing number (USABA), charges SLEV, one transaction. The FedNow participant must accept it.",
			fed("FDN-OK-1", "2500.00", "USD"), usd, "accept (ACSP)"),
		sc("fednow-host", "fednow-over-limit", "FedNow — above the $1,000,000 network limit",
			"A transfer of 1,500,000.00 USD from an account holding 5,000,000.00. The FedNow network limit for a customer credit transfer is $1,000,000: it must be refused with AM02.",
			fed("FDN-LIM-1", "1500000.00", "USD"), with(usd, "transactionLimit", "1000000.00", "expectReason", "AM02"), "reject (AM02)"),
		sc("fednow-host", "fednow-at-limit", "FedNow — exactly the network limit",
			"A transfer of exactly 1,000,000.00 USD, the network limit. It must be accepted (boundary case).",
			fed("FDN-LIM-2", "1000000.00", "USD"), with(usd, "transactionLimit", "1000000.00"), "accept (ACSP)"),
		sc("fednow-host", "fednow-not-dollars", "FedNow — a transfer in euro",
			"FedNow settles in US dollars only. A EUR instruction must be refused with AM03.",
			fed("FDN-EUR-1", "2500.00", "EUR"), with(usd, "accountCurrency", "USD", "expectReason", "AM03"), "reject (AM03)"),
		sc("fednow-host", "fednow-bad-routing-number", "FedNow — creditor routing number fails its check digit",
			"A transfer to routing number 021000022, whose ABA check digit is wrong. The participant must refuse it with RC01 (bank identifier incorrect).",
			with(fed("FDN-RTN-1", "2500.00", "USD"), "CreditRTN", "021000022"), with(usd, "expectReason", "RC01"), "reject (RC01)"),
	}
}
