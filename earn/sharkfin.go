/*
FILE: earn/sharkfin.go

DESCRIPTION:
Shark Fin (structured product) sub-client — /api/v2/earn/sharkfin/...
This file is a stub in P4-E1 (struct + constructor); P4-E2 wires the REST
surface (product / account / assets / records / subscribe-info /
subscribe / subscribe-result).
*/

package earn

// SharkFinClient — earn shark-fin sub-client. Built once per earn.Client
// and safe for concurrent use.
type SharkFinClient struct {
	c *Client
}

func newSharkFinClient(c *Client) *SharkFinClient {
	return &SharkFinClient{c: c}
}
