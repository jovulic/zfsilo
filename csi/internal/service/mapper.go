package service

import (
	"slices"
	"strings"
)

func toVolumeID(name string) string {
	return "vol_" + name
}

func toVolumeName(name string) string {
	return "volumes/" + toVolumeID(name)
}

func toDatasetID(name string, parentDatasetID string) string {
	return parentDatasetID + "/" + toVolumeID(name)
}

func parseNodeID(nodeID string) map[string]string {
	ids := make(map[string]string)
	parts := strings.SplitSeq(nodeID, ";")
	for part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			ids[kv[0]] = kv[1]
		}
	}
	return ids
}

func buildNodeID(ids map[string]string) string {
	var parts []string
	for k, v := range ids {
		parts = append(parts, k+"="+v)
	}
	slices.Sort(parts) // for stability
	return strings.Join(parts, ";")
}

func findNodeIDByClientID(knownClientIDs []string, clientID string) string {
	for _, nodeID := range knownClientIDs {
		ids := parseNodeID(nodeID)
		for _, id := range ids {
			if id == clientID {
				return nodeID
			}
		}
	}
	return clientID // fallback
}
