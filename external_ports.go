package main

import (
	"encoding/json"
	"os"
)

// externalXrayConfigs lists system-level Xray configurations installed by
// other tools. fanout never writes these files, but it avoids inbound ports
// they already use so the two installations do not collide and prevent Xray
// from starting.
//
// This currently covers byJoey/xray-cf-lite, which installs Xray as a system
// service with a fixed configuration path.
var externalXrayConfigs = []string{
	"/usr/local/etc/xray/config.json",
}

// externalUsedPorts reads inbound listen ports from external Xray configs.
//
// This is read-only. Missing or invalid files are silently skipped so fanout
// behaves exactly the same on systems without these tools.
func externalUsedPorts() map[int]bool {
	used := map[int]bool{}
	for _, path := range externalXrayConfigs {
		mergeXrayConfigPorts(path, used)
	}
	return used
}

func mergeXrayConfigPorts(path string, used map[int]bool) {
	blob, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var cfg struct {
		Inbounds []struct {
			// Xray ports can be numbers or range strings such as "1000-2000".
			// Parse each RawMessage leniently: record numeric ports and skip
			// non-numeric values so one range does not invalidate the whole array.
			Port json.RawMessage `json:"port"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(blob, &cfg); err != nil {
		return
	}
	for _, ib := range cfg.Inbounds {
		var p int
		if err := json.Unmarshal(ib.Port, &p); err == nil && p > 0 && p < 65536 {
			used[p] = true
		}
	}
}
