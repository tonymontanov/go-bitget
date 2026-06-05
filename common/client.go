/*
FILE: common/client.go

DESCRIPTION:
Root client for the Bitget V2 COMMON / PUBLIC profile. Holds a reference
to the parent bitget.Client (REST, signer, logger, config) and exposes the
category sub-clients — Public, Account, Tax, P2P, Users.
*/

package common

import (
	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
)

// Client — Bitget V2 COMMON / PUBLIC profile client. Safe for concurrent
// use; sub-clients are read-only after construction.
type Client struct {
	parent *bitget.Client

	public  *PublicClient
	account *AccountClient
	tax     *TaxClient
	p2p     *P2PClient
	users   *UserClient
}

// NewClient creates a COMMON profile client off the given parent. Returns
// nil if parent is nil — every public method guards against that so a nil
// propagation does not panic in caller code.
func NewClient(parent *bitget.Client) *Client {
	if parent == nil {
		return nil
	}
	var c *Client = &Client{parent: parent}
	c.public = newPublicClient(c)
	c.account = newAccountClient(c)
	c.tax = newTaxClient(c)
	c.p2p = newP2PClient(c)
	c.users = newUserClient(c)
	return c
}

// Parent returns the root bitget.Client.
func (c *Client) Parent() *bitget.Client { return c.parent }

// Public returns the unsigned public-utility sub-client
// (/api/v2/public/...).
func (c *Client) Public() *PublicClient { return c.public }

// Account returns the account-wide assets / trade-rate sub-client
// (/api/v2/account/... + /api/v2/common/trade-rate).
func (c *Client) Account() *AccountClient { return c.account }

// Tax returns the tax-records sub-client (/api/v2/tax/...).
func (c *Client) Tax() *TaxClient { return c.tax }

// P2P returns the P2P merchant sub-client (/api/v2/p2p/...).
func (c *Client) P2P() *P2PClient { return c.p2p }

// Users returns the virtual sub-account management sub-client
// (/api/v2/user/...).
func (c *Client) Users() *UserClient { return c.users }

// Internal shortcut shared by the sub-clients.
func (c *Client) rest() bgcommon.RestDoer { return c.parent.REST() }

// init registers the factory in the root package so that
// bitget.Client.Common() lazily returns *common.Client. A blank import of
// "github.com/tonymontanov/go-bitget/v2/common" pulls this init() in.
func init() {
	bitget.RegisterCommonFactory(func(parent *bitget.Client) any {
		return NewClient(parent)
	})
}
