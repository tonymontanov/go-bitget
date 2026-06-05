/*
FILE: common/users.go

DESCRIPTION:
Users sub-client — virtual sub-account + API-key management
(/api/v2/user/...). Implemented in milestone P6-M3.
*/

package common

// UserClient — virtual sub-account management sub-client.
type UserClient struct {
	c *Client
}

func newUserClient(c *Client) *UserClient {
	return &UserClient{c: c}
}
