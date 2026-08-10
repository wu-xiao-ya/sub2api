package service

import (
	"sort"
	"strconv"
	"strings"
)

// monitorProbeGroupKey returns the scheduling and public-status aggregation key.
// Empty group names retain legacy one-monitor-per-task behavior. Image monitors
// also stay independent because real image generation is billable.
func monitorProbeGroupKey(m *ChannelMonitor) string {
	if m == nil {
		return ""
	}
	groupName := strings.TrimSpace(m.GroupName)
	if groupName == "" || defaultAPIMode(m.APIMode) == MonitorAPIModeImages {
		return "monitor:" + strconv.FormatInt(m.ID, 10)
	}
	return strings.Join([]string{
		"group",
		groupName,
		strings.TrimSpace(m.Provider),
		defaultAPIMode(m.APIMode),
		strings.TrimSpace(m.PrimaryModel),
	}, "\x00")
}

// groupMonitorCandidates partitions compatible monitors into stable probe
// groups. Deterministic order keeps any capped group predictable.
func groupMonitorCandidates(monitors []*ChannelMonitor) [][]*ChannelMonitor {
	groupsByKey := make(map[string][]*ChannelMonitor, len(monitors))
	for _, m := range monitors {
		if m == nil || !m.Enabled {
			continue
		}
		key := monitorProbeGroupKey(m)
		if key != "" {
			groupsByKey[key] = append(groupsByKey[key], m)
		}
	}

	keys := make([]string, 0, len(groupsByKey))
	for key := range groupsByKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([][]*ChannelMonitor, 0, len(keys))
	for _, key := range keys {
		group := groupsByKey[key]
		sort.Slice(group, func(i, j int) bool {
			return group[i].ID < group[j].ID
		})
		out = append(out, group)
	}
	return out
}

func limitMonitorCandidates(monitors []*ChannelMonitor) []*ChannelMonitor {
	if len(monitors) <= monitorGroupProbeMaxCandidates {
		return monitors
	}
	return monitors[:monitorGroupProbeMaxCandidates]
}

func monitorGroupDisplayName(m *ChannelMonitor) string {
	if m == nil {
		return ""
	}
	if groupName := strings.TrimSpace(m.GroupName); groupName != "" {
		return groupName
	}
	return m.Name
}
