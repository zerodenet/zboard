package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/jobrun"
	"github.com/zerodenet/zboard/backend/internal/model"
)

const TaskCapability = "zboard.task.v1"

type TaskDefinition struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	IntervalSeconds int    `json:"interval_seconds"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
}

func (m Manifest) validateTasks() error {
	if len(m.Contributions.Tasks) > 32 {
		return errors.New("at most 32 plugin tasks are allowed")
	}
	declared := slices.Contains(m.Capabilities, TaskCapability)
	if declared && (m.Components.Server == nil || len(m.Contributions.Tasks) == 0) {
		return errors.New("task capability requires a server and task declarations")
	}
	seen := map[string]bool{}
	for _, task := range m.Contributions.Tasks {
		if !declared || !pagePattern.MatchString(task.ID) || seen[task.ID] || strings.TrimSpace(task.Title) == "" || len(task.Title) > 160 || task.IntervalSeconds < 10 || task.IntervalSeconds > 604800 || task.TimeoutSeconds < 1 || task.TimeoutSeconds > 300 {
			return errors.New("invalid plugin task declaration or limits")
		}
		seen[task.ID] = true
	}
	return nil
}

type TaskView struct {
	jobrun.Snapshot
	PluginID       string `json:"plugin_id"`
	PluginName     string `json:"plugin_name"`
	TaskID         string `json:"task_id"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}
type taskCatalogRow struct {
	ID, Name, State, Manifest, Capabilities, AuthorizedDigest, VersionID string
	Enabled, NativeTrusted                                               bool
	Generation, ConfigRevision                                           uint64
}
type declaredTask struct {
	view       TaskView
	owner      taskCatalogRow
	definition TaskDefinition
	admitted   bool
}

func (m *Manager) taskCatalog(ctx context.Context) ([]declaredTask, error) {
	var rows []taskCatalogRow
	err := m.db.WithContext(ctx).Model(&model.PluginInstallation{}).
		Select("plugin_installations.id, plugin_installations.name, plugin_installations.state, plugin_installations.enabled, plugin_installations.generation, plugin_installations.config_revision, plugin_installations.version_id, plugin_versions.manifest, plugin_authorizations.capabilities, plugin_authorizations.digest AS authorized_digest, plugin_authorizations.native_trusted").
		Joins("JOIN plugin_versions ON plugin_versions.id = plugin_installations.version_id").
		Joins("LEFT JOIN plugin_authorizations ON plugin_authorizations.plugin_id = plugin_installations.id").
		Where("plugin_installations.state <> ?", "uninstalled").Order("plugin_installations.id").Limit(200).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := []declaredTask{}
	for _, row := range rows {
		var manifest Manifest
		var capabilities []string
		if err := json.Unmarshal([]byte(row.Manifest), &manifest); err != nil {
			return nil, err
		}
		if err := manifest.validateTasks(); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(row.Capabilities), &capabilities)
		admitted := row.NativeTrusted && row.AuthorizedDigest == row.VersionID && slices.Equal(capabilities, manifest.Capabilities) && slices.Contains(capabilities, TaskCapability)
		for _, task := range manifest.Contributions.Tasks {
			out = append(out, declaredTask{view: TaskView{Snapshot: jobrun.Snapshot{ID: "plugin:" + row.ID + ":" + task.ID, Name: task.Title, Kind: "plugin", IntervalSeconds: float64(task.IntervalSeconds), Concurrency: 1, State: "idle", LastResult: "never"}, PluginID: row.ID, PluginName: row.Name, TaskID: task.ID, TimeoutSeconds: task.TimeoutSeconds}, owner: row, definition: task, admitted: admitted})
		}
	}
	return out, nil
}
