package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// ValidateRedemptionExpiry rejects an expiry timestamp in the past. Zero means
// the code never expires.
func ValidateRedemptionExpiry(expired int64) error {
	if expired != 0 && expired < common.GetTimestamp() {
		return common.NewMessage("Expiration time cannot be earlier than current time")
	}
	return nil
}

// CreateRedemptionBatch inserts count redemption codes for a user. It returns
// the keys created so far even on failure, so the caller can report which
// codes were already issued (the endpoint's historical behaviour).
func CreateRedemptionBatch(userID int, name string, count, quota int, expiredTime int64) ([]string, error) {
	var keys []string
	for range count {
		key := common.GetUUID()
		redemption := model.Redemption{
			UserId:      userID,
			Name:        name,
			Key:         key,
			CreatedTime: common.GetTimestamp(),
			Quota:       quota,
			ExpiredTime: expiredTime,
		}
		if err := redemption.Insert(); err != nil {
			return keys, err
		}
		keys = append(keys, key)
	}
	return keys, nil
}
