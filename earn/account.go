/*
FILE: earn/account.go

DESCRIPTION:
Earn account-overview sub-client — GET /api/v2/earn/account/assets:
the per-coin balance held across all earn products.
*/

package earn

import (
	"context"
	"net/url"

	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	"github.com/tonymontanov/go-bitget/v2/internal/rest"

	earntypes "github.com/tonymontanov/go-bitget/v2/earn/types"
)

// AccountClient — earn account-overview sub-client.
type AccountClient struct {
	c *Client
}

func newAccountClient(c *Client) *AccountClient {
	return &AccountClient{c: c}
}

type earnAssetRow struct {
	Coin   string `json:"coin"`
	Amount string `json:"amount"`
}

// GetAssets returns the Earn account overview: the amount held per coin
// across all earn products. Pass coin == "" for all coins.
func (a *AccountClient) GetAssets(ctx context.Context, coin string) ([]earntypes.EarnAsset, error) {
	var query url.Values
	if coin != "" {
		query = url.Values{}
		query.Set("coin", coin)
	}

	var resp rest.Response
	var err error
	resp, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/earn/account/assets",
		Query:  query,
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return nil, err
	}

	var rows []earnAssetRow
	if err = resp.UnmarshalData(&rows); err != nil {
		return nil, errParse("Account.GetAssets", err)
	}
	var out []earntypes.EarnAsset = make([]earntypes.EarnAsset, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var a2 earntypes.EarnAsset = earntypes.EarnAsset{Coin: rows[i].Coin}
		if a2.Amount, err = bgcommon.ParseDecimalOrZero(rows[i].Amount); err != nil {
			return nil, errParse("Account.GetAssets", err)
		}
		out = append(out, a2)
	}
	return out, nil
}
