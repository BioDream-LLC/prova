GO      ?= go
CXX     ?= clang++
BIN     := bin/prova
PROOFBIN := bin/prova-proof
PKG     := ./cmd/prova
# The released version, written here so a build from a source archive reports the same version.
VERSION ?= v0.1.0
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build run test vet fmt check clean web manual proof release casebook

all: check build proof

build:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) $(PKG)

# proof builds the prova-proof companion binary (independent SAT-backed prover). prova finds it
# alongside its own executable; without it, prova certifies verdicts in-process.
#
# If CADICAL points at a built CaDiCaL tree (one containing build/libcadical.a and src/cadical.hpp),
# the companion is linked against it and uses CaDiCaL — the same CDCL solver quorum cross-checks with
# — as its SAT backend. Otherwise it is built with the self-contained DPLL and needs no dependency.
CADICAL ?= $(firstword $(wildcard $(HOME)/cadical))

proof:
	@if [ -n "$(CADICAL)" ] && [ -f "$(CADICAL)/build/libcadical.a" ] && [ -f "$(CADICAL)/src/cadical.hpp" ]; then \
		echo "prova-proof: linking CaDiCaL from $(CADICAL)"; \
		$(CXX) -std=c++17 -O2 -DPROVA_USE_CADICAL -I"$(CADICAL)/src" -o $(PROOFBIN) proof/prova-proof.cpp "$(CADICAL)/build/libcadical.a"; \
	else \
		echo "prova-proof: built-in DPLL backend (set CADICAL=/path/to/cadical to link the SAT solver)"; \
		$(CXX) -std=c++17 -O2 -o $(PROOFBIN) proof/prova-proof.cpp; \
	fi

run: build
	./$(BIN)

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -l -w .

# check fails on unformatted code rather than rewriting it.
check: vet test
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

clean:
	rm -rf bin

# web rebuilds the interface into internal/web/dist, which is committed so `go build` needs no Node.
web:
	cd web && pnpm install --frozen-lockfile && pnpm build

# manual writes the reference manual (the chapters in internal/manual/chapters plus the reference generated from the
# binary) to docs/manual as one HTML file, and prints it to a PDF when a headless Chrome is available. The running
# server serves the same manual at /manual.
manual:
	mkdir -p docs/manual
	go run ./cmd/prova manual -out docs/manual/prova-manual.html
	sh scripts/print-pdf.sh docs/manual/prova-manual.html docs/manual/prova-manual.pdf

# release builds per-platform bundles under dist/release (see scripts/release.sh).
release:
	bash scripts/release.sh $(VERSION)

# casebook recompiles quorum's payments casebook with cbc (quorum's compiler), which exhaustively
# validates every state before emitting the framework; the .af and fingerprint are committed and
# embedded, so normal builds do not need cbc.
CBC ?= $(firstword $(wildcard $(HOME)/quorum/build/casebook/compiler/cbc))
casebook:
	@test -n "$(CBC)" || { echo "casebook: set CBC=/path/to/quorum/build/casebook/compiler/cbc"; exit 1; }
	$(CBC) internal/qcasebook/payments.cb --af internal/qcasebook/payments.af > /tmp/prova-cbc.txt
	@grep -E "^fingerprint" /tmp/prova-cbc.txt | awk '{print $$2}' > internal/qcasebook/payments.fingerprint
	@grep -E "Decided|fingerprint|OK " /tmp/prova-cbc.txt
