package model

import (
	"context"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// QuotaReconcileStats 是开机对账的结果统计，用于启动日志审计。
type QuotaReconcileStats struct {
	TokensChecked  int
	TokensAdjusted int
	UsersChecked   int
	UsersAdjusted  int
	SkippedFenced  int
	Failures       int
}

// ReconcileQuotaCacheOnBoot 在启动时把 Redis 中的实时余额与数据库对账，
// 补回上次崩溃/断电丢失的批量落库窗口。调用方（main）保证在开始服务
// 流量之前执行一次，因此对账期间没有并发写入，不存在 ABA 竞态。
//
// 设计依据（缺一不可，改动前请重读）：
//  1. 正常路径下 Redis 永远领先数据库半步：预扣走 Lua 原子减，DB 靠
//     批量刷盘追赶；充值同样先记 Redis（异步）再进批量队列。所以
//     diff = redis - db 恒为“丢失窗口”，符号可正可负，直接应用。
//  2. 已知残留 race：db=true 的直写路径是“先落库、异步刷缓存”，若恰在
//     两者之间崩溃，对账会误减一笔。窗口仅微秒级、金额以单笔为界，
//     每次调整都打审计日志，可事后核对。
//  3. fence 生效中的键一律跳过（正有变更在途），下一轮定时刷会收敛它。
//  4. 用户 used_quota/request_count 与渠道 used_quota 在 Redis 没有真相
//     来源（仅批量内存），崩溃即永久丢失——它们是统计口径，接受漂移，
//     这里只恢复作为钱的 quota。
//  5. 仅 master 执行：多节点同时跑会重复应用同一差值。
func ReconcileQuotaCacheOnBoot() (QuotaReconcileStats, error) {
	var stats QuotaReconcileStats
	if !common.BatchUpdateEnabled || !common.RedisEnabled || common.RDB == nil {
		return stats, nil
	}
	if !common.IsMasterNode {
		return stats, nil
	}

	ctx := context.Background()
	if err := reconcileTokenQuotas(ctx, &stats); err != nil {
		return stats, err
	}
	if err := reconcileUserQuotas(ctx, &stats); err != nil {
		return stats, err
	}
	return stats, nil
}

// scanHashKeys 按模式分页列出数据哈希键。fence 键本身（形如 token:fence:*）
// 在这里顺手过滤；“数据在、fence 也在”的并发变更则由各 reconcile 函数
// 逐键判定（见下）。
func scanHashKeys(ctx context.Context, pattern string) ([]string, error) {
	var keys []string
	var cursor uint64
	for {
		batch, next, err := common.RDB.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, err
		}
		for _, key := range batch {
			if strings.Contains(key, ":fence:") {
				continue
			}
			keys = append(keys, key)
		}
		cursor = next
		if cursor == 0 {
			return keys, nil
		}
	}
}

func parseQuotaField(fields map[string]string, name string) (int64, bool) {
	raw, ok := fields[name]
	if !ok || raw == "" {
		return 0, false
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

// reconcileTokenQuotas 对账令牌余额：Redis 哈希是真相，数据库追赶它。
func reconcileTokenQuotas(ctx context.Context, stats *QuotaReconcileStats) error {
	keys, err := scanHashKeys(ctx, "token:*")
	if err != nil {
		return err
	}
	for _, key := range keys {
		fields, err := common.RDB.HGetAll(ctx, key).Result()
		if err != nil || len(fields) == 0 {
			continue
		}
		if fenced, err := common.RDB.Exists(ctx, "token:fence:"+strings.TrimPrefix(key, "token:")).Result(); err == nil && fenced > 0 {
			// 正有变更在途：此时快照可能新旧交错，跳过，下一轮定时刷收敛。
			stats.SkippedFenced++
			continue
		}
		idRaw, ok := fields["Id"]
		if !ok {
			continue
		}
		id, err := strconv.Atoi(idRaw)
		if err != nil || id <= 0 {
			continue
		}
		redisRemain, okRemain := parseQuotaField(fields, "RemainQuota")
		redisUsed, okUsed := parseQuotaField(fields, "UsedQuota")
		if !okRemain || !okUsed {
			continue
		}
		var stored struct {
			RemainQuota int64
			UsedQuota   int64
		}
		if err := DB.Model(&Token{}).Where("id = ? AND deleted_at IS NULL", id).Select("remain_quota", "used_quota").First(&stored).Error; err != nil {
			// 行不存在或已被软删除：无处可补，跳过。
			continue
		}
		stats.TokensChecked++
		diffRemain := redisRemain - stored.RemainQuota
		diffUsed := redisUsed - stored.UsedQuota
		if diffRemain == 0 && diffUsed == 0 {
			continue
		}
		if err := DB.Model(&Token{}).Where("id = ?", id).Updates(map[string]any{
			"remain_quota": gorm.Expr("remain_quota + ?", diffRemain),
			"used_quota":   gorm.Expr("used_quota + ?", diffUsed),
		}).Error; err != nil {
			stats.Failures++
			common.SysLog(common.LogText("quota reconcile: failed to adjust token %d: %s", id, err.Error()))
			continue
		}
		stats.TokensAdjusted++
		common.SysLog(common.LogText("quota reconcile: token %d remain %+d used %+d", id, diffRemain, diffUsed))
	}
	return nil
}

// reconcileUserQuotas 对账用户钱包余额，语义同令牌。
func reconcileUserQuotas(ctx context.Context, stats *QuotaReconcileStats) error {
	keys, err := scanHashKeys(ctx, "user:*")
	if err != nil {
		return err
	}
	for _, key := range keys {
		id, err := strconv.Atoi(strings.TrimPrefix(key, "user:"))
		if err != nil || id <= 0 {
			continue
		}
		if fenced, err := common.RDB.Exists(ctx, getUserAuthFenceKey(id)).Result(); err == nil && fenced > 0 {
			stats.SkippedFenced++
			continue
		}
		fields, err := common.RDB.HGetAll(ctx, key).Result()
		if err != nil || len(fields) == 0 {
			continue
		}
		redisQuota, ok := parseQuotaField(fields, "Quota")
		if !ok {
			continue
		}
		var stored struct {
			Quota int64
		}
		if err := DB.Model(&User{}).Where("id = ? AND deleted_at IS NULL", id).Select("quota").First(&stored).Error; err != nil {
			continue
		}
		stats.UsersChecked++
		if diff := redisQuota - stored.Quota; diff != 0 {
			if err := DB.Model(&User{}).Where("id = ?", id).Update("quota", gorm.Expr("quota + ?", diff)).Error; err != nil {
				stats.Failures++
				common.SysLog(common.LogText("quota reconcile: failed to adjust user %d: %s", id, err.Error()))
				continue
			}
			stats.UsersAdjusted++
			common.SysLog(common.LogText("quota reconcile: user %d quota %+d", id, diff))
		}
	}
	return nil
}
