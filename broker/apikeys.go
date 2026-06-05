/*
FILE: broker/apikeys.go

DESCRIPTION:
API-key sub-client — /api/v2/broker/manage/... Sub-account API-key
lifecycle (create / list / modify). Implemented in milestone P5-M2.
*/

package broker

// APIKeyClient — broker sub-account API-key sub-client.
type APIKeyClient struct {
	c *Client
}

func newAPIKeyClient(c *Client) *APIKeyClient {
	return &APIKeyClient{c: c}
}
