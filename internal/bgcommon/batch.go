/*
FILE: internal/bgcommon/batch.go

DESCRIPTION:
Profile-agnostic batch trading wire shapes shared by mix/, spot/ and
(future) uta/. Bitget V2 uses ONE response envelope across every
batch endpoint:

  - /api/v2/mix/order/batch-place-order
  - /api/v2/mix/order/batch-cancel-orders
  - /api/v2/spot/trade/batch-orders
  - /api/v2/spot/trade/cancel-batch-orders
  - /api/v2/spot/trade/batch-cancel-replace-order

The envelope is `{ successList, failureList }` of per-row outcomes;
each outcome carries OrderID + ClientOid (success) or OrderID +
ClientOid + ErrorMsg + ErrorCode (failure). Profile-specific request
bodies wrap this same envelope on the response, so the SDK decodes
into the shared types and lets each profile collate the per-row
outcomes against its typed OrderInfo / BatchOrderResult.

WHY HERE:

Without this shared shape, mix/ and spot/ would each redeclare the
exact same struct layout — the literal "no parallel copy-paste" rule
codified in the v1.2.2 refactor. Lifting the wire types into bgcommon
keeps the protocol layer a single source of truth.

VALIDATION:

ValidateBatchSize() lives here too because the cap (50 rows) is a
venue-wide V2 invariant — every batch endpoint enforces it server-
side and rejects with code=40034. The helper takes a label so the
caller can pass its own qualified path
("mix.Trading.CreateBatchOrders", "spot.Trading.ModifyBatchOrders",
...) for diagnostic output.
*/

package bgcommon

import (
	"strconv"

	"github.com/tonymontanov/go-bitget/v2/internal/bgerr"
)

// MaxBatchSize — Bitget V2 cap on every batch trading endpoint (place /
// modify / cancel, mix and spot alike). Enforced client-side so requests
// never round-trip just to be rejected by the server.
const MaxBatchSize = 50

// BatchSuccessRow mirrors one row of the `successList` array returned
// by every Bitget V2 batch trading endpoint.
type BatchSuccessRow struct {
	OrderID   string `json:"orderId"`
	ClientOid string `json:"clientOid"`
}

// BatchFailureRow mirrors one row of the `failureList` array.
type BatchFailureRow struct {
	OrderID   string `json:"orderId"`
	ClientOid string `json:"clientOid"`
	ErrorMsg  string `json:"errorMsg"`
	ErrorCode string `json:"errorCode"`
}

// BatchEnvelope is the standard response shape for every batch
// trading endpoint on Bitget V2. Both mix and spot decode into this
// type and then collate the per-row outcomes against their typed
// request slice.
type BatchEnvelope struct {
	SuccessList []BatchSuccessRow `json:"successList"`
	FailureList []BatchFailureRow `json:"failureList"`
}

// ValidateBatchSize fails when n is outside [1, MaxBatchSize]. label
// is the caller-qualified method path used in the diagnostic message
// (e.g. "spot.Trading.CreateBatchOrders").
func ValidateBatchSize(label string, n int) error {
	if n == 0 {
		return bgerr.New(bgerr.ErrorKindInvalidRequest, "", label+": empty request slice", nil)
	}
	if n > MaxBatchSize {
		return bgerr.New(bgerr.ErrorKindInvalidRequest, "", label+": batch size "+strconv.Itoa(n)+" exceeds Bitget V2 cap of "+strconv.Itoa(MaxBatchSize), nil)
	}
	return nil
}
