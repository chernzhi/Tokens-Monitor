package main

import "strings"

func effectiveMonitorHosts(cfg *Config) map[string]string {
	var source map[string]string
	if cfg != nil && cfg.MonitorHosts != nil {
		source = cfg.MonitorHosts
	} else {
		source = aiDomains
	}
	hosts := make(map[string]string, len(source)+8)
	for host, vendor := range source {
		host = normalizeDomainPattern(host)
		vendor = strings.TrimSpace(vendor)
		if host != "" && vendor != "" {
			hosts[host] = vendor
		}
	}
	if cfg != nil {
		for host, vendor := range cfg.ExtraMonitorHosts {
			host = normalizeDomainPattern(host)
			vendor = strings.TrimSpace(vendor)
			if host != "" && vendor != "" {
				hosts[host] = vendor
			}
		}
	}
	return hosts
}

func effectiveMonitorSuffixes(cfg *Config) []MonitoredSuffix {
	var source []MonitoredSuffix
	if cfg != nil && cfg.MonitorSuffixes != nil {
		source = cfg.MonitorSuffixes
	} else {
		source = builtinMonitorSuffixes()
	}
	rules := make([]MonitoredSuffix, 0, len(source)+8)
	for _, rule := range source {
		rule = normalizeMonitoredSuffix(rule)
		if rule.Suffix != "" && rule.Vendor != "" {
			rules = append(rules, rule)
		}
	}
	if cfg != nil {
		for _, rule := range cfg.ExtraMonitorSuffixes {
			rule = normalizeMonitoredSuffix(rule)
			if rule.Suffix != "" && rule.Vendor != "" {
				rules = append(rules, rule)
			}
		}
	}
	return rules
}

func builtinMonitorSuffixes() []MonitoredSuffix {
	rules := make([]MonitoredSuffix, 0, len(aiWildcardDomains))
	for _, rule := range aiWildcardDomains {
		rules = append(rules, MonitoredSuffix{
			Suffix: rule.suffix,
			Prefix: rule.prefix,
			Vendor: rule.vendor,
		})
	}
	return rules
}

func effectiveBypassDomains(cfg *Config) []string {
	var source []string
	if cfg != nil && cfg.BypassDomains != nil {
		source = cfg.BypassDomains
	} else {
		source = bypassDomains
	}
	seen := make(map[string]struct{}, len(source)+len(mandatoryBypassDomains)+8)
	domains := make([]string, 0, len(source)+len(mandatoryBypassDomains)+8)
	add := func(domain string) {
		domain = normalizeBypassDomain(domain)
		key := strings.ToLower(domain)
		if domain != "" {
			if _, ok := seen[key]; ok {
				return
			}
			seen[key] = struct{}{}
			domains = append(domains, domain)
		}
	}
	for _, domain := range mandatoryBypassDomains {
		add(domain)
	}
	for _, domain := range source {
		add(domain)
	}
	if cfg != nil {
		for _, domain := range cfg.ExtraBypassDomains {
			add(domain)
		}
	}
	return domains
}

func normalizeMonitoredSuffix(rule MonitoredSuffix) MonitoredSuffix {
	return MonitoredSuffix{
		Suffix: normalizeDomainPattern(rule.Suffix),
		Prefix: normalizeDomainPattern(rule.Prefix),
		Vendor: strings.TrimSpace(rule.Vendor),
	}
}

func normalizeDomainPattern(value string) string {
	return strings.TrimSuffix(strings.TrimSpace(strings.ToLower(value)), ".")
}

func normalizeBypassDomain(value string) string {
	return strings.TrimSpace(value)
}
