// Package adapter defines the protocol-adapter SPI: the single thing a protocol implementer writes.
//
// Everything above this interface — the scenario DSL, scheduler, synthetic data, correlation and
// reporting — is protocol-agnostic. A new protocol (FIX, ISO 8583, ISO 20022) is implemented once as
// a ProtocolAdapter and registered; nothing in the core changes.
package adapter

import "context"

// Target is where a scenario drives traffic.
type Target struct {
	Addr   string            `json:"addr"`
	Params map[string]string `json:"params,omitempty"`
}

// SessionConfig carries protocol-specific session settings (credentials, sender/target comp IDs,
// scheme, framing header length, and so on).
type SessionConfig struct {
	Params map[string]string `json:"params,omitempty"`
}

// Template is a message template the adapter turns into wire bytes, filled with a Record of data.
type Template struct {
	Name string `json:"name"`
	Body string `json:"body"`
}

// Record is one row of (synthetic or supplied) data bound into a template.
type Record map[string]string

// Handle identifies a sent message so its response can be correlated.
type Handle struct {
	ID string `json:"id"`
}

// Decoded is the normalized, assertable form every verdict backend understands.
//
//	FIX:         Fields["35"] == "D"
//	ISO 8583:    Fields["DE39"] == "00"
//	ISO 20022:   Fields["Document.FIToFICstmrCdtTrf.GrpHdr.MsgId"]
type Decoded struct {
	Fields map[string]string `json:"fields"`
	Raw    []byte            `json:"-"`
	Meta   map[string]string `json:"meta,omitempty"` // latency, status, session info
}

// Conn is a live session with a target.
type Conn interface {
	Encode(tmpl Template, data Record) ([]byte, error)
	Send(ctx context.Context, msg []byte) (Handle, error)
	Await(ctx context.Context, h Handle) (Decoded, error)
	Close() error
}

// ProtocolAdapter is implemented once per protocol.
type ProtocolAdapter interface {
	Name() string
	Connect(ctx context.Context, target Target, session SessionConfig) (Conn, error)
}

// registry holds the adapters compiled into this build.
var registry = map[string]ProtocolAdapter{}

// Register makes an adapter available by name. Call from an adapter package's init.
func Register(a ProtocolAdapter) { registry[a.Name()] = a }

// Get returns a registered adapter.
func Get(name string) (ProtocolAdapter, bool) {
	a, ok := registry[name]
	return a, ok
}

// Names lists the registered adapters.
func Names() []string {
	out := make([]string, 0, len(registry))
	for n := range registry {
		out = append(out, n)
	}
	return out
}
