/*
FILE: internal/ws/protocol.go

DESCRIPTION:
On-the-wire types for the Bitget WebSocket protocol. The set of frames is
small and mostly schema-only (no logic), kept here so json-iterator can
decode incoming frames in one pass.

OUTBOUND OPS we issue:

	{"op":"login","args":[{"apiKey":"...","passphrase":"...","timestamp":"...","sign":"..."}]}
	{"op":"subscribe",  "args":[{"instType":"USDT-FUTURES","channel":"books","instId":"BTCUSDT"}, ...]}
	{"op":"unsubscribe","args":[{"instType":"USDT-FUTURES","channel":"books","instId":"BTCUSDT"}, ...]}

Plus a plain-text "ping" sent every PingInterval as a TEXT frame body
(NOT a JSON object). Bitget echoes a plain-text "pong" back.

INBOUND ENVELOPES we observe:

  Acks (login / subscribe / unsubscribe):
    {"event":"login",      "code":"0"}
    {"event":"subscribe",  "arg":{...},          "code":"0"}
    {"event":"unsubscribe","arg":{...},          "code":"0"}
    {"event":"error",      "code":"30001","msg":"..."}

  Push (typed by arg.channel):
    {"action":"snapshot","arg":{"instType":"USDT-FUTURES","channel":"books5","instId":"BTCUSDT"},
     "data":[...],"ts":1700000000000,"checksum":-12345}

The Envelope struct below captures the union; the dispatcher distinguishes
by which fields are populated (Event for control, Action for push).
*/

package ws

import "github.com/tonymontanov/go-bitget/v2/internal/codec"

// SubscriptionArg identifies a single Bitget WS subscription on the wire.
//
// Bitget separates topics into THREE coordinates instead of a single
// dotted topic string (Bybit-style). The same triplet appears in
// subscribe / unsubscribe ops and in the "arg" field of every push.
//
// Examples:
//
//	{InstType:"USDT-FUTURES", Channel:"books5", InstID:"BTCUSDT"}    // public
//	{InstType:"USDT-FUTURES", Channel:"ticker", InstID:"BTCUSDT"}    // public
//	{InstType:"USDT-FUTURES", Channel:"orders", InstID:"default"}    // private
//	{InstType:"USDT-FUTURES", Channel:"positions", Coin:"USDT"}      // private (coin-scoped)
//
// Coin is mostly used by private wallet/balance channels; for trading
// channels InstID is the symbol or "default" depending on the channel.
type SubscriptionArg struct {
	InstType string `json:"instType,omitempty"`
	Channel  string `json:"channel,omitempty"`
	InstID   string `json:"instId,omitempty"`
	Coin     string `json:"coin,omitempty"`
}

// Key returns a stable, sortable identifier for the arg. Used as the map
// key inside the subscription registry and as the de-dup key for resubscribe.
func (a SubscriptionArg) Key() string {
	// instType:channel:instId:coin (any of the parts may be empty).
	return a.InstType + ":" + a.Channel + ":" + a.InstID + ":" + a.Coin
}

// loginArgs is the JSON payload of the "login" op's args[0]. Defined as a
// concrete struct so jsoniter does not have to reflect over an `any` map.
type loginArgs struct {
	APIKey     string `json:"apiKey"`
	Passphrase string `json:"passphrase"`
	Timestamp  string `json:"timestamp"`
	Sign       string `json:"sign"`
}

// outboundOp is the JSON payload for a control frame we send to Bitget.
// args is `any` because login carries [loginArgs] while subscribe carries
// []SubscriptionArg.
type outboundOp struct {
	Op   string `json:"op"`
	Args any    `json:"args,omitempty"`
}

// Envelope captures the union of inbound frames.
//
//   - For control frames (login / subscribe / unsubscribe / error):
//     Event/Code/Msg are populated; Action and Topic are empty.
//   - For data pushes:
//     Action ("snapshot" | "update") and Arg/Data are populated; Event empty.
//   - For pong frames Bitget sends a plain-text body "pong" — handled
//     before the JSON unmarshal step in conn.go.
type Envelope struct {
	// Control fields.
	Event string `json:"event"`
	Code  string `json:"code"`
	Msg   string `json:"msg"`
	// Data fields.
	Action   string          `json:"action"`
	Arg      SubscriptionArg `json:"arg"`
	Data     codec.RawJSON   `json:"data"`
	TsMs     int64           `json:"ts"`
	Checksum int64           `json:"checksum"`
}

// IsControl returns true when the envelope describes a control-channel
// reply (event != "" and action == "").
func (e *Envelope) IsControl() bool {
	return e.Event != "" && e.Action == ""
}

// IsPush returns true when the envelope describes a data push
// (action != "" and arg.channel != "").
func (e *Envelope) IsPush() bool {
	return e.Action != "" && e.Arg.Channel != ""
}
