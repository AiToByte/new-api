package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// Upstream model-update channel scanner seam. The controller owns scheduling
// and change detection; the batched channel reads and settings writes run
// through here so the controller never references model.DB directly
// (Phase 3.2 layering).

// ListEnabledChannelsForUpstreamUpdate returns a keyset-paginated batch of
// enabled channels (id asc), projected to selectFields.
func ListEnabledChannelsForUpstreamUpdate(selectFields []string, lastID, batchSize int) ([]*model.Channel, error) {
	var channels []*model.Channel
	query := model.DB.
		Select(selectFields).
		Where("status = ?", common.ChannelStatusEnabled).
		Order("id asc").
		Limit(batchSize)
	if lastID > 0 {
		query = query.Where("id > ?", lastID)
	}
	err := query.Find(&channels).Error
	return channels, err
}

// CountEnabledChannels returns the number of currently enabled channels.
func CountEnabledChannels() (int64, error) {
	var count int64
	err := model.DB.Model(&model.Channel{}).Where("status = ?", common.ChannelStatusEnabled).Count(&count).Error
	return count, err
}

// UpdateChannelUpstreamModelSettings persists the (already-mutated) channel's
// other-settings and, when the model list changed, the models column too.
func UpdateChannelUpstreamModelSettings(channel *model.Channel, updateModels bool) error {
	updates := map[string]any{"settings": channel.OtherSettings}
	if updateModels {
		updates["models"] = channel.Models
	}
	return model.DB.Model(&model.Channel{}).Where("id = ?", channel.Id).Updates(updates).Error
}
