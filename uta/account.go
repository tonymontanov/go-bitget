/*
FILE: uta/account.go

DESCRIPTION:
Account sub-client — unified-account info / settings (/api/v3/account/...).
Implemented in milestone P7-M3.
*/

package uta

// AccountClient — unified-account sub-client.
type AccountClient struct {
	c *Client
}

func newAccountClient(c *Client) *AccountClient {
	return &AccountClient{c: c}
}
