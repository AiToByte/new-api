package model

import (
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// 渠道搜索查询构造器。
//
// 历史上 controller.SearchChannels 先把匹配渠道全量查进内存，再在 Go 里做
// status 过滤 → type 统计 → type 过滤 → 切片分页（台账 3.6）。渠道数达到数千
// 时，每次后台列表轮询都要全表加载并分配全部 Channel 结构体（含 key、
// settings、param_override 等大字段），成为 O(N) 的内存与 GC 压力。
//
// 这里把可下推的纯列谓词收敛到查询构造器中，让 controller 用
// Count / Group(type) / Limit+Offset 直接向数据库要「这一页」的数据。
// 三个方言的差异（PostgreSQL 列名需双引号）沿用 SearchChannels 既有逻辑。

// channelSearchColumns 返回搜索条件里需要按方言加引号的列名。
//
// 这里自行判定方言而不复用 commonKeyCol 等包级变量：后者依赖启动时
// initCol() 的初始化顺序，查询构造器应自包含，避免测试或早期调用构造出
// 空列名导致 SQL 语法错误。判定逻辑与 initCol 保持一致。
func channelSearchColumns() (modelsCol string, baseURLCol string, keyCol string) {
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		return `"models"`, `"base_url"`, `"key"`
	}
	return "`models`", "`base_url`", "`key`"
}

// NewChannelSearchQuery 构造渠道搜索查询，可被继续叠加排序、分页、投影。
//
// 参数语义与 NewChannelListQuery 对齐：
//   - statusFilter：-1 全部、common.ChannelStatusEnabled 仅启用、0 仅停用
//   - typeFilter：>=0 时按渠道类型过滤，负数表示不过滤
//
// 返回的查询已 Omit("key")，与旧 SearchChannels 一致，避免把渠道密钥
// 读入内存或随 API 响应泄露。调用方不得在结果上再 Select("key")。
func NewChannelSearchQuery(keyword string, group string, modelKeyword string, statusFilter int, typeFilter int) *gorm.DB {
	modelsCol, baseURLCol, keyCol := channelSearchColumns()

	// 与旧 SearchChannels / SearchTags 完全一致的搜索谓词：id 精确匹配、
	// 名称/地址模糊匹配、key 精确匹配，并限定模型名包含 modelKeyword。
	whereClause := "(id = ? OR name LIKE ? OR " + keyCol + " = ? OR " + baseURLCol + " LIKE ?) AND " + modelsCol + " LIKE ?"
	args := []any{
		common.String2Int(keyword),
		"%" + keyword + "%",
		keyword,
		"%" + keyword + "%",
		"%" + modelKeyword + "%",
	}

	query := DB.Model(&Channel{}).Omit("key").Where(whereClause, args...)
	query = ApplyChannelGroupFilter(query, group)
	query = applyChannelStatusFilter(query, statusFilter)
	if typeFilter >= 0 {
		query = query.Where("type = ?", typeFilter)
	}
	return query
}

// ChannelTypeCounts 按渠道类型统计数量，用于前端筛选器的 type_counts。
//
// 调用方必须传入「不含 type 过滤」的查询：与 GetAllChannels 既有行为一致，
// type_counts 反映的是当前搜索词 + 状态条件下的各类型总量，切换 type
// 筛选时计数不应跳变。
func ChannelTypeCounts(query *gorm.DB, dst *[]ChannelTypeCount) error {
	return query.Select("type, count(*) as count").Group("type").Find(dst).Error
}

// ChannelTypeCount 是一条 GROUP BY type 的结果行。字段名为 Type/Count，
// GORM 会按列名映射到结构体字段（GetAllChannels 亦使用同样的匿名结构）。
type ChannelTypeCount struct {
	Type  int64
	Count int64
}
