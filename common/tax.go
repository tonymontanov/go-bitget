/*
FILE: common/tax.go

DESCRIPTION:
Tax sub-client — tax transaction records (/api/v2/tax/{spot,future,margin,
p2p}-record). Implemented in milestone P6-M2.
*/

package common

// TaxClient — tax transaction-records sub-client.
type TaxClient struct {
	c *Client
}

func newTaxClient(c *Client) *TaxClient {
	return &TaxClient{c: c}
}
