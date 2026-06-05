/*
FILE: earn/elite.go

DESCRIPTION:
On-Chain Elite sub-client — /api/v2/earn/elite/... This file is a stub in
P4-E1 (struct + constructor); P4-E2 wires the REST surface (product /
assets / records / subscribe-info / subscribe / subscribe-result /
redeem-info / redeem).
*/

package earn

// EliteClient — earn on-chain-elite sub-client. Built once per
// earn.Client and safe for concurrent use.
type EliteClient struct {
	c *Client
}

func newEliteClient(c *Client) *EliteClient {
	return &EliteClient{c: c}
}
