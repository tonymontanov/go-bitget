/*
FILE: broker/client.go

DESCRIPTION:
Root client for the Bitget V2 BROKER / AGENT profile. Holds a reference to
the parent bitget.Client (REST, signer, logger, config) and exposes the
category sub-clients — SubAccounts, APIKeys, Stats, Agent, CopyBroker.
*/

package broker

import (
	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
)

// Client — Bitget V2 BROKER / AGENT profile client. Safe for concurrent
// use; sub-clients are read-only after construction.
type Client struct {
	parent *bitget.Client

	subAccounts *SubAccountClient
	apiKeys     *APIKeyClient
	stats       *StatsClient
	agent       *AgentClient
	copyBroker  *CopyBrokerClient
}

// NewClient creates a BROKER profile client off the given parent. Returns
// nil if parent is nil — every public method guards against that so a nil
// propagation does not panic in caller code.
func NewClient(parent *bitget.Client) *Client {
	if parent == nil {
		return nil
	}
	var c *Client = &Client{parent: parent}
	c.subAccounts = newSubAccountClient(c)
	c.apiKeys = newAPIKeyClient(c)
	c.stats = newStatsClient(c)
	c.agent = newAgentClient(c)
	c.copyBroker = newCopyBrokerClient(c)
	return c
}

// Parent returns the root bitget.Client.
func (c *Client) Parent() *bitget.Client { return c.parent }

// SubAccounts returns the sub-account lifecycle sub-client
// (/api/v2/broker/account/... + deposit/withdrawal records).
func (c *Client) SubAccounts() *SubAccountClient { return c.subAccounts }

// APIKeys returns the sub-account API-key sub-client
// (/api/v2/broker/manage/...).
func (c *Client) APIKeys() *APIKeyClient { return c.apiKeys }

// Stats returns the institutional-broker reporting sub-client
// (/api/v2/broker/{subaccounts,commissions,trade-volume,...}).
func (c *Client) Stats() *StatsClient { return c.stats }

// Agent returns the affiliate/referral reporting sub-client
// (/api/v2/broker/customer-*, sub-customer-list, agent-commission).
func (c *Client) Agent() *AgentClient { return c.agent }

// CopyBroker returns the copy-trading broker reads sub-client
// (/api/v2/copy/mix-broker/...).
func (c *Client) CopyBroker() *CopyBrokerClient { return c.copyBroker }

// Internal shortcut shared by the sub-clients.
func (c *Client) rest() bgcommon.RestDoer { return c.parent.REST() }

// init registers the factory in the root package so that
// bitget.Client.Broker() lazily returns *broker.Client. A blank import of
// "github.com/tonymontanov/go-bitget/v2/broker" pulls this init() in.
func init() {
	bitget.RegisterBrokerFactory(func(parent *bitget.Client) any {
		return NewClient(parent)
	})
}
