/*
FILE: uta/position.go

DESCRIPTION:
Position sub-client — positions (/api/v3/position/... + max-open-available).
Implemented in milestone P7-M5.
*/

package uta

// PositionClient — positions sub-client.
type PositionClient struct {
	c *Client
}

func newPositionClient(c *Client) *PositionClient {
	return &PositionClient{c: c}
}
