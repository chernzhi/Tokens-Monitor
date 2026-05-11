package main

import (
	"sort"
	"strings"
)

// monitorHostsForPAC returns the list of AI domain patterns that the PAC file
// should route through the local MITM proxy. Everything else goes DIRECT or
// through the user's original proxy chain — never through our MITM.
//
// The list is derived from the effective monitor_hosts + monitor_suffixes config
// so that the PAC stays in sync with the MITM CONNECT handler in proxy.go.
func monitorHostsForPAC(cfg *Config) []monitorHostEntry {
	hosts := effectiveMonitorHosts(cfg)
	suffixes := effectiveMonitorSuffixes(cfg)
	entries := make([]monitorHostEntry, 0, len(hosts)+len(suffixes))

	// Exact hostnames from config/effective table (sorted for deterministic PAC output)
	exactHosts := make([]string, 0, len(hosts))
	for host := range hosts {
		host = strings.TrimSpace(host)
		if host != "" {
			exactHosts = append(exactHosts, host)
		}
	}
	sort.Strings(exactHosts)
	for _, host := range exactHosts {
		entries = append(entries, monitorHostEntry{Kind: mhExact, Pattern: host})
	}

	for _, w := range suffixes {
		suffix := strings.TrimSpace(w.Suffix)
		prefix := strings.TrimSpace(w.Prefix)
		if suffix == "" {
			continue
		}
		if prefix == "" {
			entries = append(entries, monitorHostEntry{Kind: mhSuffix, Pattern: suffix})
		} else {
			entries = append(entries, monitorHostEntry{Kind: mhPrefixSuffix, Prefix: prefix, Pattern: suffix})
		}
	}

	return entries
}

type monitorHostKind int

const (
	mhExact        monitorHostKind = iota // host === "api.openai.com"
	mhSuffix                              // host ends with ".openai.azure.com"
	mhPrefixSuffix                        // host starts with "bedrock-runtime." AND ends with ".amazonaws.com"
)

type monitorHostEntry struct {
	Kind    monitorHostKind
	Pattern string // exact hostname or suffix
	Prefix  string // only for mhPrefixSuffix
}
