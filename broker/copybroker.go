/*
FILE: broker/copybroker.go

DESCRIPTION:
CopyBroker sub-client — copy-trading broker reads
(/api/v2/copy/mix-broker/{query-traders,query-history-traces,
query-current-traces}). Implemented in milestone P5-M5.
*/

package broker

// CopyBrokerClient — copy-trading broker reads sub-client.
type CopyBrokerClient struct {
	c *Client
}

func newCopyBrokerClient(c *Client) *CopyBrokerClient {
	return &CopyBrokerClient{c: c}
}
