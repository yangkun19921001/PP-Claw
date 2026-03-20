package security

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// blockedNetworks contains private/internal IP ranges that should not be accessed.
var blockedNetworks []*net.IPNet

func init() {
	cidrs := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"100.64.0.0/10",
		"::1/128",
		"fc00::/7",
		"fe80::/10",
	}
	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(cidr)
		if err == nil {
			blockedNetworks = append(blockedNetworks, network)
		}
	}
}

// isBlockedIP checks if an IP address falls within any blocked network.
func isBlockedIP(ip net.IP) bool {
	for _, network := range blockedNetworks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// ValidateURLTarget validates a URL is safe to fetch (not targeting internal networks).
// It resolves the hostname and checks all resulting IPs.
// Returns (safe bool, reason string).
func ValidateURLTarget(rawURL string) (bool, string) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false, fmt.Sprintf("invalid URL: %v", err)
	}

	host := parsed.Hostname()
	if host == "" {
		return false, "empty hostname"
	}

	// Direct IP check
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return false, fmt.Sprintf("blocked IP: %s", ip)
		}
		return true, ""
	}

	// DNS resolution
	ips, err := net.LookupIP(host)
	if err != nil {
		return false, fmt.Sprintf("DNS resolution failed for %s: %v", host, err)
	}

	for _, ip := range ips {
		if isBlockedIP(ip) {
			return false, fmt.Sprintf("hostname %s resolves to blocked IP: %s", host, ip)
		}
	}

	return true, ""
}

// ValidateResolvedURL validates a URL after redirect (checks the final destination).
// Used in HTTP redirect callbacks.
func ValidateResolvedURL(rawURL string) (bool, string) {
	return ValidateURLTarget(rawURL)
}

// ContainsInternalURL scans a command string for URLs pointing to internal networks.
// Returns true if any internal URL is found.
func ContainsInternalURL(command string) bool {
	// Simple heuristic: look for URL-like patterns and validate them
	words := strings.Fields(command)
	for _, word := range words {
		if strings.HasPrefix(word, "http://") || strings.HasPrefix(word, "https://") {
			if safe, _ := ValidateURLTarget(word); !safe {
				return true
			}
		}
	}
	return false
}
