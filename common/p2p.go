/*
FILE: common/p2p.go

DESCRIPTION:
P2P sub-client — P2P merchant info / orders / advertisements
(/api/v2/p2p/{merchantList,merchantInfo,orderList,advList}). Implemented in
milestone P6-M2.
*/

package common

// P2PClient — P2P merchant sub-client.
type P2PClient struct {
	c *Client
}

func newP2PClient(c *Client) *P2PClient {
	return &P2PClient{c: c}
}
