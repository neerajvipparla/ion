// Package fields provides blockchain-specific logging field helpers.
//
// These helpers create structured fields with consistent naming for
// blockchain-related data like transactions, blocks, shards, and timing.
//
// Usage:
//
//	import "github.com/JupiterMetaLabs/ion/fields"
//
//	logger.Info(ctx, "transaction routed",
//	    fields.TxHash("abc123"),
//	    fields.ShardID(5),
//	    fields.LatencyMs(12.5),
//	)
package fields

import "github.com/neerajvipparla/ion"

// --- Transaction Fields -----------------------------------------------------

// TxHash creates a transaction hash field.
func TxHash(hash string) ion.Field { return ion.String("tx_hash", hash) }

// TxType creates a transaction type field.
func TxType(typ string) ion.Field { return ion.String("tx_type", typ) }

// TxStatus creates a transaction status field.
func TxStatus(status string) ion.Field { return ion.String("tx_status", status) }

// TxSignature creates a transaction signature field.
func TxSignature(sig string) ion.Field { return ion.String("tx_signature", sig) }

// Nonce creates a transaction nonce field.
func Nonce(n uint64) ion.Field { return ion.Uint64("nonce", n) }

// Value creates a transaction value field.
func Value(val string) ion.Field { return ion.String("value", val) }

// FromAddress creates a sender address field.
func FromAddress(addr string) ion.Field { return ion.String("from_address", addr) }

// ToAddress creates a recipient address field.
func ToAddress(addr string) ion.Field { return ion.String("to_address", addr) }

// GasLimit creates a gas limit field.
func GasLimit(limit uint64) ion.Field { return ion.Uint64("gas_limit", limit) }

// GasPrice creates a gas price field.
func GasPrice(price uint64) ion.Field { return ion.Uint64("gas_price", price) }

// GasUsed creates a gas used field.
func GasUsed(used uint64) ion.Field { return ion.Uint64("gas_used", used) }

// --- Block & Consensus Fields -----------------------------------------------

// BlockHeight creates a block height field.
func BlockHeight(h uint64) ion.Field { return ion.Uint64("block_height", h) }

// BlockHash creates a block hash field.
func BlockHash(h string) ion.Field { return ion.String("block_hash", h) }

// Slot creates a consensus slot number field.
func Slot(s uint64) ion.Field { return ion.Uint64("slot", s) }

// Epoch creates a consensus epoch field.
func Epoch(e uint64) ion.Field { return ion.Uint64("epoch", e) }

// Round creates a consensus round field.
func Round(r uint64) ion.Field { return ion.Uint64("round", r) }

// View creates a consensus view field.
func View(v uint64) ion.Field { return ion.Uint64("view", v) }

// Proposer creates a block proposer ID field.
func Proposer(id string) ion.Field { return ion.String("proposer_id", id) }

// Validator creates a validator ID field.
func Validator(id string) ion.Field { return ion.String("validator_id", id) }

// ShardID creates a shard ID field.
func ShardID(id int) ion.Field { return ion.Int("shard_id", id) }

// --- Network & P2P Fields ---------------------------------------------------

// ChainID creates a chain ID field.
func ChainID(id string) ion.Field { return ion.String("chain_id", id) }

// Network creates a network name field (mainnet, testnet).
func Network(net string) ion.Field { return ion.String("network", net) }

// NodeID creates a node ID field.
func NodeID(id string) ion.Field { return ion.String("node_id", id) }

// PeerID creates a peer ID field.
func PeerID(id string) ion.Field { return ion.String("peer_id", id) }

// ClientID creates a client ID field (e.g. user-agent).
func ClientID(id string) ion.Field { return ion.String("client_id", id) }

// RemoteAddr creates a remote address field.
func RemoteAddr(addr string) ion.Field { return ion.String("remote_addr", addr) }

// Host creates a host field.
func Host(h string) ion.Field { return ion.String("host", h) }

// Port creates a port field.
func Port(p int) ion.Field { return ion.Int("port", p) }

// --- Component & Operation Fields -------------------------------------------

// Component creates a component name field.
func Component(name string) ion.Field { return ion.String("component", name) }

// Operation creates an operation name field.
func Operation(op string) ion.Field { return ion.String("operation", op) }

// Method creates a method name field.
func Method(m string) ion.Field { return ion.String("method", m) }

// Protocol creates a protocol version/name field.
func Protocol(p string) ion.Field { return ion.String("protocol", p) }

// --- Metrics & Stats Fields -------------------------------------------------

// Count creates a generic count field.
func Count(n int) ion.Field { return ion.Int("count", n) }

// Total creates a total count field.
func Total(n int) ion.Field { return ion.Int("total", n) }

// Size creates a size field in bytes.
func Size(bytes int64) ion.Field { return ion.Int64("size_bytes", bytes) }

// Pending creates a pending count field.
func Pending(n int) ion.Field { return ion.Int("pending_count", n) }

// LatencyMs creates a latency field in milliseconds.
func LatencyMs(ms float64) ion.Field { return ion.Float64("latency_ms", ms) }

// DurationMs creates a duration field in milliseconds.
func DurationMs(ms float64) ion.Field { return ion.Float64("duration_ms", ms) }

// Weight creates a weight field.
func Weight(w float64) ion.Field { return ion.Float64("weight", w) }

// Score creates a score field.
func Score(s float64) ion.Field { return ion.Float64("score", s) }

// --- Application Logic Fields -----------------------------------------------

// Address creates a generic address field (wallet/contract).
func Address(addr string) ion.Field { return ion.String("address", addr) }

// Contract creates a contract address field.
func Contract(addr string) ion.Field { return ion.String("contract", addr) }

// Token creates a token symbol/address field.
func Token(t string) ion.Field { return ion.String("token", t) }

// Amount creates a generic amount field.
func Amount(amt string) ion.Field { return ion.String("amount", amt) }

// Reason creates a reason field.
func Reason(r string) ion.Field { return ion.String("reason", r) }

// Success creates a success boolean field.
func Success(ok bool) ion.Field { return ion.Bool("success", ok) }

// Enabled creates an enabled boolean field.
func Enabled(on bool) ion.Field { return ion.Bool("enabled", on) }

// ErrorMsg creates a detailed error message field.
func ErrorMsg(err error) ion.Field {
	if err == nil {
		return ion.String("error", "")
	}
	return ion.String("error", err.Error())
}
