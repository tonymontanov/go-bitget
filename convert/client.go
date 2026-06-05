/*
FILE: convert/client.go

DESCRIPTION:
Root client for the Bitget V2 CONVERT profile. Holds a reference to the
parent bitget.Client (REST, signer, logger, config) and exposes the
flash-swap + BGB-convert REST surface directly (the profile is small
enough not to warrant sub-clients).
*/

package convert

import (
	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
)

// Client — Bitget V2 CONVERT profile client. Safe for concurrent use.
type Client struct {
	parent *bitget.Client
}

// NewClient creates a CONVERT profile client off the given parent.
// Returns nil if parent is nil — every public method guards against
// that so a nil propagation does not panic in caller code.
func NewClient(parent *bitget.Client) *Client {
	if parent == nil {
		return nil
	}
	return &Client{parent: parent}
}

// Parent returns the root bitget.Client.
func (c *Client) Parent() *bitget.Client { return c.parent }

// Internal shortcuts shared by the methods — mirror of the
// mix/spot/margin/copytrading helpers.
func (c *Client) rest() bgcommon.RestDoer { return c.parent.REST() }

// init registers the factory in the root package so that
// bitget.Client.Convert() lazily returns *convert.Client. A blank import
// of "github.com/tonymontanov/go-bitget/v2/convert" pulls this init()
// into the binary.
func init() {
	bitget.RegisterConvertFactory(func(parent *bitget.Client) any {
		return NewClient(parent)
	})
}
