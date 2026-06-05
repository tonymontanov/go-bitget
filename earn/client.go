/*
FILE: earn/client.go

DESCRIPTION:
Root client for the Bitget V2 EARN profile. Holds a reference to the
parent bitget.Client (REST, signer, logger, config) and exposes the
category sub-clients — Account, Savings, SharkFin, Elite, Loan.
*/

package earn

import (
	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
)

// Client — Bitget V2 EARN profile client. Safe for concurrent use;
// sub-clients are read-only after construction.
type Client struct {
	parent *bitget.Client

	account  *AccountClient
	savings  *SavingsClient
	sharkfin *SharkFinClient
	elite    *EliteClient
	loan     *LoanClient
}

// NewClient creates an EARN profile client off the given parent. Returns
// nil if parent is nil — every public method guards against that so a
// nil propagation does not panic in caller code.
func NewClient(parent *bitget.Client) *Client {
	if parent == nil {
		return nil
	}
	var c *Client = &Client{parent: parent}
	c.account = newAccountClient(c)
	c.savings = newSavingsClient(c)
	c.sharkfin = newSharkFinClient(c)
	c.elite = newEliteClient(c)
	c.loan = newLoanClient(c)
	return c
}

// Parent returns the root bitget.Client.
func (c *Client) Parent() *bitget.Client { return c.parent }

// Account returns the Earn account-overview sub-client
// (/api/v2/earn/account/...).
func (c *Client) Account() *AccountClient { return c.account }

// Savings returns the savings sub-client (/api/v2/earn/savings/...).
func (c *Client) Savings() *SavingsClient { return c.savings }

// SharkFin returns the shark-fin sub-client (/api/v2/earn/sharkfin/...).
func (c *Client) SharkFin() *SharkFinClient { return c.sharkfin }

// Elite returns the on-chain-elite sub-client (/api/v2/earn/elite/...).
func (c *Client) Elite() *EliteClient { return c.elite }

// Loan returns the crypto-loan sub-client (/api/v2/earn/loan/...).
func (c *Client) Loan() *LoanClient { return c.loan }

// Internal shortcut shared by the sub-clients.
func (c *Client) rest() bgcommon.RestDoer { return c.parent.REST() }

// init registers the factory in the root package so that
// bitget.Client.Earn() lazily returns *earn.Client. A blank import of
// "github.com/tonymontanov/go-bitget/v2/earn" pulls this init() in.
func init() {
	bitget.RegisterEarnFactory(func(parent *bitget.Client) any {
		return NewClient(parent)
	})
}
