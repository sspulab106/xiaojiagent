// Package geo resolves approximate human-readable regions from IP addresses
// using the free ip-api.com service. It is a soft dependency: any failure
// returns "" so callers degrade gracefully.
package geo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Region returns a short region label (e.g. "HK · 香港 · 香港岛") for an IP.
// Private/loopback addresses and any lookup failure yield "".
func Region(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" || ip == "127.0.0.1" || ip == "::1" || strings.EqualFold(ip, "localhost") {
		return ""
	}
	u := fmt.Sprintf("http://ip-api.com/json/%s?fields=status,regionName,city,countryCode&lang=zh-CN",
		url.QueryEscape(ip))
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(u)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var g struct {
		Status      string `json:"status"`
		CountryCode string `json:"countryCode"`
		RegionName  string `json:"regionName"`
		City        string `json:"city"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&g); err != nil || g.Status != "success" {
		return ""
	}
	var parts []string
	if g.CountryCode != "" {
		parts = append(parts, g.CountryCode)
	}
	if g.RegionName != "" && g.RegionName != g.CountryCode {
		parts = append(parts, g.RegionName)
	}
	if g.City != "" && g.City != g.RegionName {
		parts = append(parts, g.City)
	}
	return strings.Join(parts, " · ")
}
