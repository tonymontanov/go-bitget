/*
FILE: uta/client.go

DESCRIPTION:
Root client for the Bitget V3 UTA profile. Holds a reference to the parent
bitget.Client (REST, signer, logger, config) and exposes the category
sub-clients — Public, Account, Trade, Position, Strategy.
*/

package uta

import (
	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
)

// Client — Bitget V3 Unified Trading Account profile client. Safe for
// concurrent use; sub-clients are read-only after construction.
type Client struct {
	parent *bitget.Client

	public   *PublicClient
	account  *AccountClient
	trade    *TradeClient
	position *PositionClient
	strategy *StrategyClient
}

// NewClient creates a UTA profile client off the given parent. Returns nil
// if parent is nil — every public method guards against that so a nil
// propagation does not panic in caller code.
func NewClient(parent *bitget.Client) *Client {
	if parent == nil {
		return nil
	}
	var c *Client = &Client{parent: parent}
	c.public = newPublicClient(c)
	c.account = newAccountClient(c)
	c.trade = newTradeClient(c)
	c.position = newPositionClient(c)
	c.strategy = newStrategyClient(c)
	return c
}

// Parent returns the root bitget.Client.
func (c *Client) Parent() *bitget.Client { return c.parent }

// Public returns the unsigned market-data sub-client (/api/v3/public,market).
func (c *Client) Public() *PublicClient { return c.public }

// Account returns the unified-account sub-client (/api/v3/account/...).
func (c *Client) Account() *AccountClient { return c.account }

// Trade returns the order-flow sub-client (/api/v3/trade/...).
func (c *Client) Trade() *TradeClient { return c.trade }

// Position returns the positions sub-client (/api/v3/position/...).
func (c *Client) Position() *PositionClient { return c.position }

// Strategy returns the plan-order sub-client (/api/v3/trade/*-strategy-*).
func (c *Client) Strategy() *StrategyClient { return c.strategy }

// Internal shortcut shared by the sub-clients.
func (c *Client) rest() bgcommon.RestDoer { return c.parent.REST() }

// init registers the factory in the root package so that
// bitget.Client.UTA() lazily returns *uta.Client. A blank import of
// "github.com/tonymontanov/go-bitget/v2/uta" pulls this init() in.
func init() {
	bitget.RegisterUTAFactory(func(parent *bitget.Client) any {
		return NewClient(parent)
	})
}
