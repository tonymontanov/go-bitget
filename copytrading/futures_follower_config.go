/*
FILE: copytrading/futures_follower_config.go

DESCRIPTION:
Futures FOLLOWER configuration surface (M2b) — the per-trader copy
settings, TP/SL on a tracked position, and the venue's copy
quantity limits.

WIRED:

	POST mix-follower/settings              — UpdateSettings
	GET  mix-follower/query-settings        — GetSettings
	POST mix-follower/setting-tpsl          — SetTPSL
	GET  mix-follower/query-quantity-limit  — GetCopyLimit

All four carry the pinned productType (UpdateSettings stamps it onto each
per-symbol row; the others send it as a query / body field).
*/

package copytrading

import (
	"context"
	"net/url"

	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	"github.com/tonymontanov/go-bitget/v2/internal/rest"
	copytypes "github.com/tonymontanov/go-bitget/v2/copytrading/types"
)

// ---------------------------------------------------------------------
// UpdateSettings — settings.
// ---------------------------------------------------------------------

type settingRowBody struct {
	Symbol           string `json:"symbol"`
	ProductType      string `json:"productType"`
	MarginType       string `json:"marginType"`
	MarginCoin       string `json:"marginCoin,omitempty"`
	LeverType        string `json:"leverType"`
	LongLeverage     string `json:"longLeverage,omitempty"`
	ShortLeverage    string `json:"shortLeverage,omitempty"`
	TraceType        string `json:"traceType"`
	TraceValue       string `json:"traceValue"`
	MaxHoldSize      string `json:"maxHoldSize,omitempty"`
	StopSurplusRatio string `json:"stopSurplusRatio,omitempty"`
	StopLossRatio    string `json:"stopLossRatio,omitempty"`
}

type settingsBody struct {
	TraderID string           `json:"traderId"`
	AutoCopy string           `json:"autoCopy,omitempty"`
	Mode     string           `json:"mode,omitempty"`
	Settings []settingRowBody `json:"settings"`
}

// UpdateSettings sets the copy-trade configuration (per symbol, max 10)
// for one lead trader. The pinned futures productType is stamped onto
// every row, so SymbolSetting omits it. TraderID and at least one
// setting are required; each setting requires Symbol / MarginType /
// LeverType / TraceType / TraceValue.
func (f *FuturesFollowerClient) UpdateSettings(ctx context.Context, req copytypes.FollowSettingsRequest) error {
	if req.TraderID == "" {
		return errInvalid("FuturesFollower.UpdateSettings", "traderID is empty")
	}
	if len(req.Settings) == 0 {
		return errInvalid("FuturesFollower.UpdateSettings", "settings is empty")
	}
	if len(req.Settings) > 10 {
		return errInvalid("FuturesFollower.UpdateSettings", "settings exceeds 10 rows")
	}

	var rows []settingRowBody = make([]settingRowBody, 0, len(req.Settings))
	var i int
	for i = 0; i < len(req.Settings); i++ {
		var s copytypes.SymbolSetting = req.Settings[i]
		if s.Symbol == "" {
			return errInvalid("FuturesFollower.UpdateSettings", "settings: symbol is empty")
		}
		if s.MarginType == "" || s.LeverType == "" || s.TraceType == "" || s.TraceValue == "" {
			return errInvalid("FuturesFollower.UpdateSettings", "settings: marginType/leverType/traceType/traceValue are required")
		}
		rows = append(rows, settingRowBody{
			Symbol:           s.Symbol,
			ProductType:      f.productType(),
			MarginType:       s.MarginType,
			MarginCoin:       s.MarginCoin,
			LeverType:        s.LeverType,
			LongLeverage:     s.LongLeverage,
			ShortLeverage:    s.ShortLeverage,
			TraceType:        s.TraceType,
			TraceValue:       s.TraceValue,
			MaxHoldSize:      s.MaxHoldSize,
			StopSurplusRatio: s.StopSurplusRatio,
			StopLossRatio:    s.StopLossRatio,
		})
	}

	var _, _, err = f.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/copy/mix-follower/settings",
		Body: settingsBody{
			TraderID: req.TraderID,
			AutoCopy: req.AutoCopy,
			Mode:     req.Mode,
			Settings: rows,
		},
		Signed: true,
		Meta:   queryMeta(),
	})
	return err
}

// ---------------------------------------------------------------------
// GetSettings — query-settings.
// ---------------------------------------------------------------------

type settingDetailRow struct {
	Symbol           string `json:"symbol"`
	ProductType      string `json:"productType"`
	MarginType       string `json:"marginType"`
	MarginCoin       string `json:"marginCoin"`
	LeverType        string `json:"leverType"`
	LongLeverage     string `json:"longLeverage"`
	ShortLeverage    string `json:"shortLeverage"`
	TraceType        string `json:"traceType"`
	TraceValue       string `json:"traceValue"`
	MaxHoldSize      string `json:"maxHoldSize"`
	StopSurplusRatio string `json:"stopSurplusRatio"`
	StopLossRatio    string `json:"stopLossRatio"`
}

type settingsEnvelope struct {
	FollowerEnable string             `json:"followerEnable"`
	DetailList     []settingDetailRow `json:"detailList"`
}

// GetSettings returns this account's copy-trade configuration for the
// given lead trader. traderID is required.
func (f *FuturesFollowerClient) GetSettings(ctx context.Context, traderID string) (copytypes.FollowSettings, error) {
	var out copytypes.FollowSettings
	if traderID == "" {
		return out, errInvalid("FuturesFollower.GetSettings", "traderID is empty")
	}

	var query url.Values = url.Values{}
	query.Set("traderId", traderID)

	var resp rest.Response
	var err error
	resp, _, err = f.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/copy/mix-follower/query-settings",
		Query:  query,
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}

	var env settingsEnvelope
	if err = resp.UnmarshalData(&env); err != nil {
		return out, errParse("FuturesFollower.GetSettings", err)
	}
	out.FollowerEnable = env.FollowerEnable
	out.Following = env.FollowerEnable == "YES"
	out.DetailList = make([]copytypes.FollowSettingDetail, 0, len(env.DetailList))
	var i int
	for i = 0; i < len(env.DetailList); i++ {
		var d copytypes.FollowSettingDetail
		d, err = convertSettingDetail(env.DetailList[i])
		if err != nil {
			return out, errParse("FuturesFollower.GetSettings", err)
		}
		out.DetailList = append(out.DetailList, d)
	}
	return out, nil
}

func convertSettingDetail(row settingDetailRow) (copytypes.FollowSettingDetail, error) {
	var out copytypes.FollowSettingDetail = copytypes.FollowSettingDetail{
		Symbol:      row.Symbol,
		ProductType: row.ProductType,
		MarginType:  row.MarginType,
		MarginCoin:  row.MarginCoin,
		LeverType:   row.LeverType,
		TraceType:   row.TraceType,
	}
	var err error
	if out.LongLeverage, err = bgcommon.ParseDecimalOrZero(row.LongLeverage); err != nil {
		return copytypes.FollowSettingDetail{}, err
	}
	if out.ShortLeverage, err = bgcommon.ParseDecimalOrZero(row.ShortLeverage); err != nil {
		return copytypes.FollowSettingDetail{}, err
	}
	if out.TraceValue, err = bgcommon.ParseDecimalOrZero(row.TraceValue); err != nil {
		return copytypes.FollowSettingDetail{}, err
	}
	if out.MaxHoldSize, err = bgcommon.ParseDecimalOrZero(row.MaxHoldSize); err != nil {
		return copytypes.FollowSettingDetail{}, err
	}
	if out.StopSurplusRatio, err = bgcommon.ParseDecimalOrZero(row.StopSurplusRatio); err != nil {
		return copytypes.FollowSettingDetail{}, err
	}
	if out.StopLossRatio, err = bgcommon.ParseDecimalOrZero(row.StopLossRatio); err != nil {
		return copytypes.FollowSettingDetail{}, err
	}
	return out, nil
}

// ---------------------------------------------------------------------
// SetTPSL — setting-tpsl.
// ---------------------------------------------------------------------

type settingTPSLBody struct {
	ProductType      string `json:"productType"`
	TrackingNo       string `json:"trackingNo"`
	Symbol           string `json:"symbol,omitempty"`
	StopSurplusPrice string `json:"stopSurplusPrice,omitempty"`
	StopLossPrice    string `json:"stopLossPrice,omitempty"`
}

// SetTPSL sets / updates / cancels the take-profit and stop-loss on a
// tracked copied position. TrackingNo is required. Price semantics:
// empty = leave unchanged, "0" = cancel existing, > 0 = set/update.
func (f *FuturesFollowerClient) SetTPSL(ctx context.Context, req copytypes.FollowTPSLRequest) error {
	if req.TrackingNo == "" {
		return errInvalid("FuturesFollower.SetTPSL", "trackingNo is empty")
	}
	var _, _, err = f.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/copy/mix-follower/setting-tpsl",
		Body: settingTPSLBody{
			ProductType:      f.productType(),
			TrackingNo:       req.TrackingNo,
			Symbol:           req.Symbol,
			StopSurplusPrice: req.StopSurplusPrice,
			StopLossPrice:    req.StopLossPrice,
		},
		Signed: true,
		Meta:   queryMeta(),
	})
	return err
}

// ---------------------------------------------------------------------
// GetCopyLimit — query-quantity-limit.
// ---------------------------------------------------------------------

type followLimitRow struct {
	Symbol        string `json:"symbol"`
	MaxFollowSize string `json:"maxFollowSize"`
	MinFollowSize string `json:"minFollowSize"`
}

// GetCopyLimit returns the venue min/max copy order size (trading
// currency) for the pinned futures product type. `symbol` is optional
// (pass "" for all symbols).
func (f *FuturesFollowerClient) GetCopyLimit(ctx context.Context, symbol string) ([]copytypes.FollowLimit, error) {
	var query url.Values = url.Values{}
	query.Set("productType", f.productType())
	if symbol != "" {
		query.Set("symbol", symbol)
	}

	var resp rest.Response
	var err error
	resp, _, err = f.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/copy/mix-follower/query-quantity-limit",
		Query:  query,
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return nil, err
	}

	var rows []followLimitRow
	if err = resp.UnmarshalData(&rows); err != nil {
		return nil, errParse("FuturesFollower.GetCopyLimit", err)
	}

	var out []copytypes.FollowLimit = make([]copytypes.FollowLimit, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var lim copytypes.FollowLimit = copytypes.FollowLimit{Symbol: rows[i].Symbol}
		if lim.MaxFollowSize, err = bgcommon.ParseDecimalOrZero(rows[i].MaxFollowSize); err != nil {
			return nil, errParse("FuturesFollower.GetCopyLimit", err)
		}
		if lim.MinFollowSize, err = bgcommon.ParseDecimalOrZero(rows[i].MinFollowSize); err != nil {
			return nil, errParse("FuturesFollower.GetCopyLimit", err)
		}
		out = append(out, lim)
	}
	return out, nil
}
