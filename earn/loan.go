/*
FILE: earn/loan.go

DESCRIPTION:
Crypto Loan sub-client — /api/v2/earn/loan/... This file is a stub in
P4-E1 (struct + constructor); P4-E3 wires the REST surface (public coin
infos / hour-interest, borrow / repay / revise-pledge, plus the ongoing /
history / debts / reduces queries).
*/

package earn

// LoanClient — earn crypto-loan sub-client. Built once per earn.Client
// and safe for concurrent use.
type LoanClient struct {
	c *Client
}

func newLoanClient(c *Client) *LoanClient {
	return &LoanClient{c: c}
}
