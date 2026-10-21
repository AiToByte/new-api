package service

import (
	"context"
	"fmt"
	"maps"
	"sort"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
)

// Task plugin runtime synchronization. The registry compiles plugin sources
// in-process; this keeps the override set and the compile tracker in sync with
// the database rows. The state, the single sync pass and the periodic loop all
// live here so controllers only trigger or read them.

// TaskPluginRebuildOutcome records the result of the most recent sync pass.
type TaskPluginRebuildOutcome struct {
	Status           string    `json:"status"`
	AttemptedAt      time.Time `json:"attempted_at"`
	Generation       uint64    `json:"generation"`
	DatabaseRevision string    `json:"database_revision,omitempty"`
	PluginErrorCount int       `json:"plugin_error_count"`
	Error            string    `json:"error,omitempty"`
}

// taskPluginSyncTracker remembers, per plugin key, the source hash last
// published into the registry and any compile error retained from the last
// sync attempt.
var taskPluginSyncState = struct {
	sync.Mutex
	hashes      map[string]string
	errors      map[string]string
	lastRebuild TaskPluginRebuildOutcome
}{hashes: map[string]string{}, errors: map[string]string{}}

// SyncTaskPluginsOnceContext runs one sync pass: it snapshots the database
// rows, recompiles every plugin whose source hash changed (retaining the
// incumbent override when compilation fails), publishes the result as one
// registry generation, and records the tracker state.
func SyncTaskPluginsOnceContext(ctx context.Context) error {
	started := time.Now()
	taskPluginSyncState.Lock()
	defer taskPluginSyncState.Unlock()
	databaseSnapshot, err := model.GetTaskPluginSyncSnapshot()
	if err != nil {
		syncErr := fmt.Errorf("sync task plugins: %w", err)
		taskPluginSyncState.lastRebuild = TaskPluginRebuildOutcome{
			Status:           "failed",
			AttemptedAt:      time.Now(),
			Generation:       jsplugin.DefaultRegistry.Generation().Number,
			DatabaseRevision: taskPluginSyncState.lastRebuild.DatabaseRevision,
			Error:            syncErr.Error(),
		}
		logger.LogDebug(
			ctx,
			"task_plugin subsystem=sync event=failed stage=database_snapshot retained_generation=%d elapsed_ms=%d",
			jsplugin.DefaultRegistry.Generation().Number,
			time.Since(started).Milliseconds(),
		)
		return syncErr
	}
	databasePlugins := databaseSnapshot.Plugins
	sort.Slice(databasePlugins, func(i, j int) bool { return databasePlugins[i].Key < databasePlugins[j].Key })
	currentOverrides := jsplugin.DefaultRegistry.OverridePlugins()
	generationBefore := jsplugin.DefaultRegistry.Generation().Number
	logger.LogDebug(
		ctx,
		"task_plugin subsystem=sync event=start database_revision=%q generation=%d desired_plugins=%d current_overrides=%d",
		databaseSnapshot.Revision,
		generationBefore,
		len(databasePlugins),
		len(currentOverrides),
	)
	nextOverrides := make([]*jsplugin.LoadedPlugin, 0, len(databasePlugins))
	nextHashes := make(map[string]string, len(databasePlugins))
	seen := make(map[string]bool, len(databasePlugins))
	for _, plugin := range databasePlugins {
		seen[plugin.Key] = true
		if current := currentOverrides[plugin.Key]; current != nil && taskPluginSyncState.hashes[plugin.Key] == plugin.SourceHash {
			nextOverrides = append(nextOverrides, current)
			nextHashes[plugin.Key] = plugin.SourceHash
			logger.LogDebug(
				ctx,
				"task_plugin subsystem=sync event=plugin plugin=%q version=%q action=reuse",
				plugin.Key,
				plugin.Version,
			)
			continue
		}
		logger.LogDebug(
			ctx,
			"task_plugin subsystem=sync event=plugin plugin=%q version=%q action=compile_start",
			plugin.Key,
			plugin.Version,
		)
		var compiled *jsplugin.LoadedPlugin
		source, compileErr := model.GetTaskPluginSource(plugin.Id)
		if compileErr != nil {
			compileErr = fmt.Errorf("load source: %w", compileErr)
		} else {
			compiled, compileErr = jsplugin.CompilePlugin(string(source), jsplugin.Options{Key: plugin.Key, Version: plugin.Version})
		}
		if compileErr != nil {
			retainedIncumbent := false
			if current := currentOverrides[plugin.Key]; current != nil {
				nextOverrides = append(nextOverrides, current)
				retainedIncumbent = true
				if currentHash := taskPluginSyncState.hashes[plugin.Key]; currentHash != "" {
					nextHashes[plugin.Key] = currentHash
				}
			}
			taskPluginSyncState.errors[plugin.Key] = compileErr.Error()
			common.SysError(common.LogText("compile task plugin %s@%s: %v", plugin.Key, plugin.Version, compileErr))
			logger.LogDebug(
				ctx,
				"task_plugin subsystem=sync event=plugin plugin=%q version=%q action=compile_failed retained_incumbent=%t",
				plugin.Key,
				plugin.Version,
				retainedIncumbent,
			)
			continue
		}
		nextOverrides = append(nextOverrides, compiled)
		nextHashes[plugin.Key] = plugin.SourceHash
		delete(taskPluginSyncState.errors, plugin.Key)
		logger.LogDebug(
			ctx,
			"task_plugin subsystem=sync event=plugin plugin=%q version=%q action=compile_success",
			plugin.Key,
			plugin.Version,
		)
	}
	if err = jsplugin.DefaultRegistry.ReplaceOverrides(nextOverrides); err != nil {
		syncErr := fmt.Errorf("publish task plugin generation: %w", err)
		taskPluginSyncState.lastRebuild = TaskPluginRebuildOutcome{
			Status:           "failed",
			AttemptedAt:      time.Now(),
			Generation:       jsplugin.DefaultRegistry.Generation().Number,
			DatabaseRevision: databaseSnapshot.Revision,
			Error:            syncErr.Error(),
		}
		logger.LogDebug(
			ctx,
			"task_plugin subsystem=sync event=failed stage=publish retained_generation=%d retained_generation_active=true database_revision=%q elapsed_ms=%d",
			jsplugin.DefaultRegistry.Generation().Number,
			databaseSnapshot.Revision,
			time.Since(started).Milliseconds(),
		)
		return syncErr
	}
	taskPluginSyncState.hashes = nextHashes
	for key := range taskPluginSyncState.errors {
		if !seen[key] {
			delete(taskPluginSyncState.errors, key)
		}
	}
	pluginErrors := jsplugin.DefaultRegistry.RoutingErrors()
	maps.Copy(pluginErrors, taskPluginSyncState.errors)
	pluginErrorCount := len(pluginErrors)
	status := "success"
	if pluginErrorCount > 0 {
		status = "partial"
	}
	taskPluginSyncState.lastRebuild = TaskPluginRebuildOutcome{
		Status:           status,
		AttemptedAt:      time.Now(),
		Generation:       jsplugin.DefaultRegistry.Generation().Number,
		DatabaseRevision: databaseSnapshot.Revision,
		PluginErrorCount: pluginErrorCount,
	}
	logger.LogDebug(
		ctx,
		"task_plugin subsystem=sync event=complete database_revision=%q previous_generation=%d generation=%d status=%q active_overrides=%d plugin_errors=%d elapsed_ms=%d",
		databaseSnapshot.Revision,
		generationBefore,
		jsplugin.DefaultRegistry.Generation().Number,
		status,
		len(jsplugin.DefaultRegistry.ActiveOverridePlugins()),
		pluginErrorCount,
		time.Since(started).Milliseconds(),
	)
	return nil
}

// SyncTaskPluginsOnce runs one sync pass with a background context.
func SyncTaskPluginsOnce() error {
	return SyncTaskPluginsOnceContext(context.Background())
}

// SyncTaskPlugins runs the periodic background sync loop. Failures are logged
// and surfaced through the tracker; the loop keeps running.
func SyncTaskPlugins() {
	syncOnce := func() {
		if err := SyncTaskPluginsOnce(); err != nil {
			common.SysError(err.Error())
		}
	}
	syncOnce()
	for range time.NewTicker(30 * time.Second).C {
		syncOnce()
	}
}

// TaskPluginSyncPluginErrors merges the tracked per-plugin compile errors into
// dst (typically the registry's routing error map).
func TaskPluginSyncPluginErrors(dst map[string]string) {
	taskPluginSyncState.Lock()
	maps.Copy(dst, taskPluginSyncState.errors)
	taskPluginSyncState.Unlock()
}

// TaskPluginSyncLastRebuild returns a copy of the last sync outcome.
func TaskPluginSyncLastRebuild() TaskPluginRebuildOutcome {
	taskPluginSyncState.Lock()
	defer taskPluginSyncState.Unlock()
	return taskPluginSyncState.lastRebuild
}

// TaskPluginSyncSetLastRebuild records the last sync outcome, e.g. to restore a
// snapshot taken before a test drives a sync pass.
func TaskPluginSyncSetLastRebuild(outcome TaskPluginRebuildOutcome) {
	taskPluginSyncState.Lock()
	taskPluginSyncState.lastRebuild = outcome
	taskPluginSyncState.Unlock()
}

// TaskPluginSyncRemember records tracker state for a plugin key. The sync pass
// keeps it current; callers (and tests exercising the refresh path) may also
// seed it.
func TaskPluginSyncRemember(key, hash, compileErr string) {
	taskPluginSyncState.Lock()
	taskPluginSyncState.hashes[key] = hash
	if compileErr == "" {
		delete(taskPluginSyncState.errors, key)
	} else {
		taskPluginSyncState.errors[key] = compileErr
	}
	taskPluginSyncState.Unlock()
}

// TaskPluginSyncTracker returns the tracker state recorded for a plugin key.
func TaskPluginSyncTracker(key string) (hash string, hasCompileErr bool) {
	taskPluginSyncState.Lock()
	defer taskPluginSyncState.Unlock()
	hash = taskPluginSyncState.hashes[key]
	_, hasCompileErr = taskPluginSyncState.errors[key]
	return hash, hasCompileErr
}

// TaskPluginSyncForget drops tracker state for a plugin key, e.g. once the
// plugin row is gone.
func TaskPluginSyncForget(key string) {
	taskPluginSyncState.Lock()
	delete(taskPluginSyncState.hashes, key)
	delete(taskPluginSyncState.errors, key)
	taskPluginSyncState.Unlock()
}
