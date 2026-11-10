package service

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/ionet"
)

// io.net model deployment integration. Option-map reads, API-key validation
// and the pure projection/counting helpers live here so the controller only
// binds requests, resolves params and renders responses.

// ErrIoNetNotConfigured is returned when the integration is disabled or no API
// key has been configured.
var ErrIoNetNotConfigured = errors.New("io.net model deployment is not enabled or api key missing")

// IoNetAPIKey returns the configured API key, checking the enabled flag.
func IoNetAPIKey() (string, error) {
	common.OptionMapRWMutex.RLock()
	enabled := common.OptionMap["model_deployment.ionet.enabled"] == "true"
	apiKey := common.OptionMap["model_deployment.ionet.api_key"]
	common.OptionMapRWMutex.RUnlock()
	if !enabled || strings.TrimSpace(apiKey) == "" {
		return "", ErrIoNetNotConfigured
	}
	return apiKey, nil
}

// StoredIoNetAPIKey returns the stored key without requiring the integration
// to be enabled — the connection test validates a candidate key this way.
func StoredIoNetAPIKey() string {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	return strings.TrimSpace(common.OptionMap["model_deployment.ionet.api_key"])
}

// ModelDeploymentSettings reports whether the integration is enabled and
// whether an API key has been configured.
func ModelDeploymentSettings() (enabled, configured bool) {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	enabled = common.OptionMap["model_deployment.ionet.enabled"] == "true"
	configured = strings.TrimSpace(common.OptionMap["model_deployment.ionet.api_key"]) != ""
	return enabled, configured
}

// ValidateIoNetAPIKey checks an API key against io.net and reports the hardware
// inventory. The raw upstream error is returned so callers can render
// ionet.APIError messages without losing detail.
func ValidateIoNetAPIKey(apiKey string) (hardwareCount, totalAvailable int, err error) {
	client := ionet.NewEnterpriseClient(apiKey)
	result, err := client.GetMaxGPUsPerContainer()
	if err != nil {
		return 0, 0, err
	}
	if result != nil {
		hardwareCount = len(result.Hardware)
		totalAvailable = result.Total
		if totalAvailable == 0 {
			for _, hw := range result.Hardware {
				totalAvailable += hw.Available
			}
		}
	}
	return hardwareCount, totalAvailable, nil
}

func MapIoNetDeployment(d ionet.Deployment) map[string]any {
	var created int64
	if d.CreatedAt.IsZero() {
		created = time.Now().Unix()
	} else {
		created = d.CreatedAt.Unix()
	}

	timeRemainingHours := d.ComputeMinutesRemaining / 60
	timeRemainingMins := d.ComputeMinutesRemaining % 60
	var timeRemaining string
	if timeRemainingHours > 0 {
		timeRemaining = fmt.Sprintf("%d hour %d minutes", timeRemainingHours, timeRemainingMins)
	} else if timeRemainingMins > 0 {
		timeRemaining = fmt.Sprintf("%d minutes", timeRemainingMins)
	} else {
		timeRemaining = "completed"
	}

	hardwareInfo := fmt.Sprintf("%s %s x%d", d.BrandName, d.HardwareName, d.HardwareQuantity)

	return map[string]any{
		"id":                        d.ID,
		"deployment_name":           d.Name,
		"container_name":            d.Name,
		"status":                    strings.ToLower(d.Status),
		"type":                      "Container",
		"time_remaining":            timeRemaining,
		"time_remaining_minutes":    d.ComputeMinutesRemaining,
		"hardware_info":             hardwareInfo,
		"hardware_name":             d.HardwareName,
		"brand_name":                d.BrandName,
		"hardware_quantity":         d.HardwareQuantity,
		"completed_percent":         d.CompletedPercent,
		"compute_minutes_served":    d.ComputeMinutesServed,
		"compute_minutes_remaining": d.ComputeMinutesRemaining,
		"created_at":                created,
		"updated_at":                created,
		"model_name":                "",
		"model_version":             "",
		"instance_count":            d.HardwareQuantity,
		"resource_config": map[string]any{
			"cpu":    "",
			"memory": "",
			"gpu":    strconv.Itoa(d.HardwareQuantity),
		},
		"description": "",
		"provider":    "io.net",
	}
}

func ComputeDeploymentStatusCounts(total int, deployments []ionet.Deployment) map[string]int64 {
	counts := map[string]int64{
		"all": int64(total),
	}

	for _, status := range []string{"running", "completed", "failed", "deployment requested", "termination requested", "destroyed"} {
		counts[status] = 0
	}

	for _, d := range deployments {
		status := strings.ToLower(strings.TrimSpace(d.Status))
		counts[status] = counts[status] + 1
	}

	return counts
}
