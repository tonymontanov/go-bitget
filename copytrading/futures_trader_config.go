/*
FILE: copytrading/futures_trader_config.go

DESCRIPTION:
Futures TRADER configuration surface (M3b) — the per-symbol copy
settings, the global lead-trader switches, and the follower roster.

WIRED:

	GET  mix-trader/config-query-symbols    — GetSymbolSettings
	POST mix-trader/config-setting-symbols  — SetSymbols
	POST mix-trader/config-settings-base    — SetGlobalSettings
	GET  mix-trader/config-query-followers  — GetFollowers (page-no paged)
	POST mix-trader/config-remove-follower  — RemoveFollower

SetSymbols stamps the pinned productType onto every row. The followers
roster uses page-number pagination (not the idLessThan cursor), so it
loops pageNo with a hard page ceiling.
*/

package copytrading

import (
	"context"
	"net/url"
	"strconv"

	copytypes "github.com/tonymontanov/go-bitget/v2/copytrading/types"
	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	"github.com/tonymontanov/go-bitget/v2/internal/rest"
)

// followersPageSize / followersMaxPages bound the page-number pagination
// of the follower roster (10k followers ceiling — defensive against a
// buggy page echo).
const (
	followersPageSize = 200
	followersMaxPages = 50
)

// ---------------------------------------------------------------------
// GetSymbolSettings — config-query-symbols.
// ---------------------------------------------------------------------

type symbolConfigRow struct {
	Symbol           string `json:"symbol"`
	OpenTrader       string `json:"openTrader"`
	MinOpenCount     string `json:"minOpenCount"`
	MaxLeverage      string `json:"maxLeverage"`
	StopSurplusRatio string `json:"stopSurplusRatio"`
	StopLossRatio    string `json:"stopLossRatio"`
}

// GetSymbolSettings returns the per-symbol copy-trade configuration the
// lead trader can broadcast on the pinned futures product type.
func (t *FuturesTraderClient) GetSymbolSettings(ctx context.Context) ([]copytypes.TraderSymbolConfig, error) {
	var query url.Values = url.Values{}
	query.Set("productType", t.productType())

	var resp rest.Response
	var err error
	resp, _, err = t.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/copy/mix-trader/config-query-symbols",
		Query:  query,
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return nil, err
	}

	var rows []symbolConfigRow
	if err = resp.UnmarshalData(&rows); err != nil {
		return nil, errParse("FuturesTrader.GetSymbolSettings", err)
	}

	var out []copytypes.TraderSymbolConfig = make([]copytypes.TraderSymbolConfig, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var cfg copytypes.TraderSymbolConfig = copytypes.TraderSymbolConfig{
			Symbol:     rows[i].Symbol,
			OpenTrader: rows[i].OpenTrader,
		}
		if cfg.MinOpenCount, err = bgcommon.ParseDecimalOrZero(rows[i].MinOpenCount); err != nil {
			return nil, errParse("FuturesTrader.GetSymbolSettings", err)
		}
		if cfg.MaxLeverage, err = bgcommon.ParseDecimalOrZero(rows[i].MaxLeverage); err != nil {
			return nil, errParse("FuturesTrader.GetSymbolSettings", err)
		}
		if cfg.StopSurplusRatio, err = bgcommon.ParseDecimalOrZero(rows[i].StopSurplusRatio); err != nil {
			return nil, errParse("FuturesTrader.GetSymbolSettings", err)
		}
		if cfg.StopLossRatio, err = bgcommon.ParseDecimalOrZero(rows[i].StopLossRatio); err != nil {
			return nil, errParse("FuturesTrader.GetSymbolSettings", err)
		}
		out = append(out, cfg)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// SetSymbols — config-setting-symbols.
// ---------------------------------------------------------------------

type symbolSettingRowBody struct {
	Symbol           string `json:"symbol"`
	ProductType      string `json:"productType"`
	SettingType      string `json:"settingType"`
	StopSurplusRatio string `json:"stopSurplusRatio,omitempty"`
	StopLossRatio    string `json:"stopLossRatio,omitempty"`
}

type settingSymbolsBody struct {
	SettingList []symbolSettingRowBody `json:"settingList"`
}

// SetSymbols adds / deletes / updates the copy-trade symbols the lead
// trader broadcasts (max 50 per call). The pinned productType is stamped
// onto every row. Each change requires Symbol and SettingType.
func (t *FuturesTraderClient) SetSymbols(ctx context.Context, changes []copytypes.SymbolSettingChange) error {
	if len(changes) == 0 {
		return errInvalid("FuturesTrader.SetSymbols", "changes is empty")
	}
	if len(changes) > 50 {
		return errInvalid("FuturesTrader.SetSymbols", "changes exceeds 50 rows")
	}

	var rows []symbolSettingRowBody = make([]symbolSettingRowBody, 0, len(changes))
	var i int
	for i = 0; i < len(changes); i++ {
		var ch copytypes.SymbolSettingChange = changes[i]
		if ch.Symbol == "" || ch.SettingType == "" {
			return errInvalid("FuturesTrader.SetSymbols", "changes: symbol/settingType are required")
		}
		rows = append(rows, symbolSettingRowBody{
			Symbol:           ch.Symbol,
			ProductType:      t.productType(),
			SettingType:      ch.SettingType,
			StopSurplusRatio: ch.StopSurplusRatio,
			StopLossRatio:    ch.StopLossRatio,
		})
	}

	var _, _, err = t.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/copy/mix-trader/config-setting-symbols",
		Body:   settingSymbolsBody{SettingList: rows},
		Signed: true,
		Meta:   queryMeta(),
	})
	return err
}

// ---------------------------------------------------------------------
// SetGlobalSettings — config-settings-base.
// ---------------------------------------------------------------------

type globalSettingsBody struct {
	Enable          string `json:"enable,omitempty"`
	ShowTotalEquity string `json:"showTotalEquity,omitempty"`
	ShowTpsl        string `json:"showTpsl,omitempty"`
}

// SetGlobalSettings flips the lead trader's global switches (activate
// elite trading, show total equity, show order TP/SL). At least one
// field must be set; all are "YES"/"NO".
func (t *FuturesTraderClient) SetGlobalSettings(ctx context.Context, req copytypes.GlobalSettingsRequest) error {
	if req.Enable == "" && req.ShowTotalEquity == "" && req.ShowTpsl == "" {
		return errInvalid("FuturesTrader.SetGlobalSettings", "one of enable / showTotalEquity / showTpsl is required")
	}
	var _, _, err = t.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/copy/mix-trader/config-settings-base",
		Body: globalSettingsBody{
			Enable:          req.Enable,
			ShowTotalEquity: req.ShowTotalEquity,
			ShowTpsl:        req.ShowTpsl,
		},
		Signed: true,
		Meta:   queryMeta(),
	})
	return err
}

// ---------------------------------------------------------------------
// GetFollowers — config-query-followers.
// ---------------------------------------------------------------------

type followerRow struct {
	AccountEquity   string `json:"accountEquity"`
	IsRemove        string `json:"isRemove"`
	FollowerHeadPic string `json:"followerHeadPic"`
	FollowerName    string `json:"followerName"`
	FollowerUID     string `json:"followerUid"`
	FollowerTime    string `json:"followerTime"`
}

// GetFollowers returns the lead trader's full follower roster. Uses
// page-number pagination internally (pageNo / pageSize), accumulating
// pages until a short page or the defensive page ceiling.
func (t *FuturesTraderClient) GetFollowers(ctx context.Context) ([]copytypes.TraderFollower, error) {
	var out []copytypes.TraderFollower
	var page int
	for page = 1; page <= followersMaxPages; page++ {
		if cerr := ctx.Err(); cerr != nil {
			return out, cerr
		}

		var query url.Values = url.Values{}
		query.Set("pageNo", strconv.Itoa(page))
		query.Set("pageSize", strconv.Itoa(followersPageSize))

		var resp rest.Response
		var err error
		resp, _, err = t.c.rest().Do(ctx, rest.Options{
			Method: "GET",
			Path:   "/api/v2/copy/mix-trader/config-query-followers",
			Query:  query,
			Signed: true,
			Meta:   queryMeta(),
		})
		if err != nil {
			return nil, err
		}

		var rows []followerRow
		if err = resp.UnmarshalData(&rows); err != nil {
			return nil, errParse("FuturesTrader.GetFollowers", err)
		}
		if len(rows) == 0 {
			break
		}

		var i int
		for i = 0; i < len(rows); i++ {
			var f copytypes.TraderFollower = copytypes.TraderFollower{
				FollowerUID:     rows[i].FollowerUID,
				FollowerName:    rows[i].FollowerName,
				FollowerHeadPic: rows[i].FollowerHeadPic,
				IsRemove:        rows[i].IsRemove,
			}
			if f.AccountEquity, err = bgcommon.ParseDecimalOrZero(rows[i].AccountEquity); err != nil {
				return nil, errParse("FuturesTrader.GetFollowers", err)
			}
			f.FollowerTimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].FollowerTime)
			out = append(out, f)
		}

		if len(rows) < followersPageSize {
			break
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------
// RemoveFollower — config-remove-follower.
// ---------------------------------------------------------------------

type removeFollowerBody struct {
	FollowerUID string `json:"followerUid"`
}

// RemoveFollower removes the given follower. followerUID is required.
func (t *FuturesTraderClient) RemoveFollower(ctx context.Context, followerUID string) error {
	if followerUID == "" {
		return errInvalid("FuturesTrader.RemoveFollower", "followerUID is empty")
	}
	var _, _, err = t.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/copy/mix-trader/config-remove-follower",
		Body:   removeFollowerBody{FollowerUID: followerUID},
		Signed: true,
		Meta:   queryMeta(),
	})
	return err
}
