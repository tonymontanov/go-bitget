/*
FILE: broker/agent.go

DESCRIPTION:
Agent sub-client — affiliate/referral reporting
(/api/v2/broker/customer-*, sub-customer-list, agent-commission).
Implemented in milestone P5-M4.
*/

package broker

// AgentClient — affiliate/referral reporting sub-client.
type AgentClient struct {
	c *Client
}

func newAgentClient(c *Client) *AgentClient {
	return &AgentClient{c: c}
}
