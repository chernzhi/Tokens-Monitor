package main

import (
	"sort"
	"strings"
)

// monitorHostsForPAC returns the list of AI domain patterns that the PAC file
// should route through the local MITM proxy. Everything else goes DIRECT or
// through the user's original proxy chain — never through our MITM.
//
// The list is derived from aiDomains (exact) + aiWildcardDomains (suffix/prefix)
// + config ExtraMonitorHosts/ExtraMonitorSuffixes so that the PAC stays in sync
// with the MITM CONNECT handler in proxy.go.
func monitorHostsForPAC(cfg *Config) []monitorHostEntry {
	entries := make([]monitorHostEntry, 0, len(aiDomains)+len(aiWildcardDomains)+16)

	// Exact hostnames from built-in table (sorted for deterministic PAC output)
	builtinHosts := make([]string, 0, len(aiDomains))
	for host := range aiDomains {
		builtinHosts = append(builtinHosts, host)
	}
	sort.Strings(builtinHosts)
	for _, host := range builtinHosts {
		entries = append(entries, monitorHostEntry{Kind: mhExact, Pattern: host})
	}

	// Suffix/prefix patterns from built-in wildcards
	for _, w := range aiWildcardDomains {
		if w.prefix == "" {
			entries = append(entries, monitorHostEntry{Kind: mhSuffix, Pattern: w.suffix})
		} else {
			entries = append(entries, monitorHostEntry{Kind: mhPrefixSuffix, Prefix: w.prefix, Pattern: w.suffix})
		}
	}

	// Config-supplied exact hosts (sorted for deterministic PAC output)
	if cfg != nil {
		extraHosts := make([]string, 0, len(cfg.ExtraMonitorHosts))
		for host := range cfg.ExtraMonitorHosts {
			host = strings.TrimSpace(host)
			if host != "" {
				extraHosts = append(extraHosts, host)
			}
		}
		sort.Strings(extraHosts)
		for _, host := range extraHosts {
			entries = append(entries, monitorHostEntry{Kind: mhExact, Pattern: host})
		}
		// Config-supplied suffix patterns
		for _, s := range cfg.ExtraMonitorSuffixes {
			suf := strings.TrimSpace(s.Suffix)
			if suf != "" {
				entries = append(entries, monitorHostEntry{Kind: mhSuffix, Pattern: suf})
			}
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
