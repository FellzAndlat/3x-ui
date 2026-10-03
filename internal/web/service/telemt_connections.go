package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

const (
	telemtConnectionMonitorInterval = 10 * time.Second
	telemtConnectionActiveWindow    = 25 * time.Second
	telemtGeoCacheTTL               = 30 * 24 * time.Hour
)

type TelemtIPLocation struct {
	IP          string  `json:"ip"`
	City        string  `json:"city"`
	Region      string  `json:"region"`
	Country     string  `json:"country"`
	CountryCode string  `json:"countryCode"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
}

type TelemtConnection struct {
	Username           string             `json:"username"`
	CurrentConnections int                `json:"currentConnections"`
	ActiveIPs          []TelemtIPLocation `json:"activeIPs"`
	RecentIPCount      int                `json:"recentIPCount"`
	FirstSeenAt        int64              `json:"firstSeenAt"`
	LastSeenAt         int64              `json:"lastSeenAt"`
	TotalBytes         int64              `json:"totalBytes"`
	Active             bool               `json:"active"`
}

type telemtUserStats struct {
	Username           string
	CurrentConnections int
	ActiveUniqueIPs    []string
	RecentUniqueIPs    []string
	TotalOctets        int64
}

var (
	telemtMonitorOnce sync.Once
	telemtRefreshMu   sync.Mutex
	telemtGeoState    struct {
		sync.Mutex
		retryUntil time.Time
	}
)

func StartTelemtConnectionMonitor() {
	telemtMonitorOnce.Do(func() {
		go func() {
			_ = refreshTelemtConnectionSnapshot()
			ticker := time.NewTicker(telemtConnectionMonitorInterval)
			defer ticker.Stop()
			for range ticker.C {
				_ = refreshTelemtConnectionSnapshot()
			}
		}()
	})
}

func (TelemtService) ConnectedClients() ([]TelemtConnection, error) {
	StartTelemtConnectionMonitor()

	db := database.GetDB()
	if db == nil {
		return []TelemtConnection{}, nil
	}

	var history []model.TelemtUserHistory
	if err := db.Order("current_connections DESC, last_seen_at DESC, id DESC").Find(&history).Error; err != nil {
		return nil, fmt.Errorf("telemt: read user history: %w", err)
	}

	allIPs := make([]string, 0)
	seenIPs := make(map[string]struct{})
	activeIPsByUser := make(map[string][]string, len(history))
	recentIPCountByUser := make(map[string]int, len(history))
	now := time.Now()
	for _, row := range history {
		active := row.LastSeenAt > 0 && now.Sub(time.UnixMilli(row.LastSeenAt)) <= telemtConnectionActiveWindow
		activeIPs := decodeTelemtIPs(row.ActiveIPs)
		recentIPs := decodeTelemtIPs(row.RecentIPs)
		if !active {
			activeIPs = []string{}
		}
		activeIPsByUser[row.Username] = activeIPs
		recentIPCountByUser[row.Username] = len(recentIPs)
		for _, ip := range activeIPs {
			if _, ok := seenIPs[ip]; ok {
				continue
			}
			seenIPs[ip] = struct{}{}
			allIPs = append(allIPs, ip)
		}
	}

	geoByIP := make(map[string]model.TelemtIPGeo, len(allIPs))
	if len(allIPs) > 0 {
		var geos []model.TelemtIPGeo
		if err := db.Where("ip IN ?", allIPs).Find(&geos).Error; err != nil {
			return nil, fmt.Errorf("telemt: read IP geolocation: %w", err)
		}
		for _, geo := range geos {
			geoByIP[geo.IP] = geo
		}
	}

	out := make([]TelemtConnection, 0, len(history))
	for _, row := range history {
		activeIPs := activeIPsByUser[row.Username]
		locations := make([]TelemtIPLocation, 0, len(activeIPs))
		for _, ip := range activeIPs {
			geo, ok := geoByIP[ip]
			if !ok {
				geo = model.TelemtIPGeo{IP: ip, City: "Неизвестно", Country: "—"}
			}
			locations = append(locations, TelemtIPLocation{
				IP:          ip,
				City:        geo.City,
				Region:      geo.Region,
				Country:     geo.Country,
				CountryCode: geo.CountryCode,
				Latitude:    geo.Latitude,
				Longitude:   geo.Longitude,
			})
		}

		active := len(activeIPs) > 0 || (row.LastSeenAt > 0 && time.Since(time.UnixMilli(row.LastSeenAt)) <= telemtConnectionActiveWindow)
		if !active {
			// Keep the API contract stable: arrays are encoded as [] instead of null.
			locations = []TelemtIPLocation{}
		}

		currentConnections := row.CurrentConnections
		if !active {
			currentConnections = 0
		}

		out = append(out, TelemtConnection{
			Username:           row.Username,
			CurrentConnections: currentConnections,
			ActiveIPs:          locations,
			RecentIPCount:      recentIPCountByUser[row.Username],
			FirstSeenAt:        row.FirstSeenAt,
			LastSeenAt:         row.LastSeenAt,
			TotalBytes:         row.TotalBytes,
			Active:             active,
		})
	}

	return out, nil
}

func refreshTelemtConnectionSnapshot() error {
	telemtRefreshMu.Lock()
	defer telemtRefreshMu.Unlock()

	if systemctl("is-active", "--quiet", telemtServiceName) != nil {
		return nil
	}

	client := &http.Client{Timeout: 4 * time.Second}
	users, err := fetchTelemtUserStats(client)
	if err != nil {
		return err
	}

	if err := persistTelemtUserSnapshot(users); err != nil {
		return err
	}

	// GeoIP is enrichment only. Do it after persistence so a slow or unavailable
	// geolocation provider never blocks user/traffic monitoring.
	_ = ensureTelemtGeo(client, collectTelemtSnapshotIPs(users))
	return nil
}

func fetchTelemtUserStats(client *http.Client) ([]telemtUserStats, error) {
	req, err := telemtAPIRequest(http.MethodGet, "/v1/users")
	if err != nil {
		return nil, fmt.Errorf("telemt: create users request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("telemt: users API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("telemt: users API returned HTTP %d", resp.StatusCode)
	}

	var envelope struct {
		OK   bool `json:"ok"`
		Data []struct {
			Username           string   `json:"username"`
			CurrentConnections int      `json:"current_connections"`
			ActiveUniqueIPs    []string `json:"active_unique_ips_list"`
			RecentUniqueIPs    []string `json:"recent_unique_ips_list"`
			TotalOctets        int64    `json:"total_octets"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("telemt: decode users: %w", err)
	}
	if !envelope.OK {
		return nil, errors.New("telemt: users API rejected request")
	}

	out := make([]telemtUserStats, 0, len(envelope.Data))
	for _, user := range envelope.Data {
		out = append(out, telemtUserStats{
			Username:           strings.TrimSpace(user.Username),
			CurrentConnections: max(user.CurrentConnections, 0),
			ActiveUniqueIPs:    normalizeTelemtIPs(user.ActiveUniqueIPs),
			RecentUniqueIPs:    normalizeTelemtIPs(user.RecentUniqueIPs),
			TotalOctets:        maxInt64(user.TotalOctets, 0),
		})
	}
	return out, nil
}

func collectTelemtSnapshotIPs(users []telemtUserStats) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, user := range users {
		for _, ip := range user.ActiveUniqueIPs {
			if _, ok := seen[ip]; ok {
				continue
			}
			seen[ip] = struct{}{}
			out = append(out, ip)
		}
	}
	return out
}

func ensureTelemtGeo(client *http.Client, ips []string) error {
	db := database.GetDB()
	if db == nil || len(ips) == 0 {
		return nil
	}

	now := time.Now()
	telemtGeoState.Lock()
	blocked := now.Before(telemtGeoState.retryUntil)
	telemtGeoState.Unlock()
	if blocked {
		return nil
	}
	var cached []model.TelemtIPGeo
	if err := db.Where("ip IN ?", ips).Find(&cached).Error; err != nil {
		return fmt.Errorf("telemt: read geolocation cache: %w", err)
	}

	knownFresh := make(map[string]struct{}, len(cached))
	missing := make([]string, 0)
	for _, geo := range cached {
		if now.Sub(time.UnixMilli(geo.LastCheckedAt)) < telemtGeoCacheTTL {
			knownFresh[geo.IP] = struct{}{}
		}
	}
	for _, ip := range ips {
		if _, ok := knownFresh[ip]; !ok {
			missing = append(missing, ip)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	for _, ip := range missing {
		geo, retryAfter, ok := lookupTelemtGeo(client, ip)
		if !ok {
			telemtGeoState.Lock()
			if !retryAfter.IsZero() {
				telemtGeoState.retryUntil = retryAfter
			} else {
				telemtGeoState.retryUntil = time.Now().Add(5 * time.Minute)
			}
			telemtGeoState.Unlock()
			return nil
		}
		telemtGeoState.Lock()
		telemtGeoState.retryUntil = time.Time{}
		telemtGeoState.Unlock()

		geo.LastCheckedAt = now.UnixMilli()
		if err := db.Where("ip = ?", ip).Assign(&geo).FirstOrCreate(&geo).Error; err != nil {
			return fmt.Errorf("telemt: save geolocation %s: %w", ip, err)
		}
	}
	return nil
}

func lookupTelemtGeo(client *http.Client, ip string) (model.TelemtIPGeo, time.Time, bool) {
	result := model.TelemtIPGeo{
		IP:      ip,
		City:    "Неизвестно",
		Country: "—",
	}

	parsed := net.ParseIP(ip)
	if parsed == nil {
		return result, time.Time{}, false
	}
	if parsed.IsPrivate() || parsed.IsLoopback() || parsed.IsLinkLocalUnicast() || parsed.IsUnspecified() {
		result.City = "Локальная сеть"
		result.Country = "Локальная сеть"
		return result, time.Time{}, true
	}

	endpoint := "https://ipwho.is/" + url.PathEscape(ip) +
		"?fields=success,message,city,region,country,country_code,latitude,longitude&lang=ru"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, endpoint, nil)
	if err != nil {
		return result, time.Time{}, false
	}

	resp, err := client.Do(req)
	if err != nil {
		return result, time.Time{}, false
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := time.Now().Add(time.Hour)
		if retry := resp.Header.Get("Retry-After"); retry != "" {
			if seconds, err := time.ParseDuration(retry + "s"); err == nil {
				retryAfter = time.Now().Add(seconds)
			}
		}
		return result, retryAfter, false
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, time.Time{}, false
	}

	var geo struct {
		Success     bool    `json:"success"`
		Message     string  `json:"message"`
		City        string  `json:"city"`
		Region      string  `json:"region"`
		Country     string  `json:"country"`
		CountryCode string  `json:"country_code"`
		Latitude    float64 `json:"latitude"`
		Longitude   float64 `json:"longitude"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&geo); err != nil || !geo.Success {
		return result, time.Time{}, false
	}

	result.City = strings.TrimSpace(geo.City)
	result.Region = strings.TrimSpace(geo.Region)
	result.Country = strings.TrimSpace(geo.Country)
	result.CountryCode = strings.TrimSpace(geo.CountryCode)
	result.Latitude = geo.Latitude
	result.Longitude = geo.Longitude
	if result.City == "" {
		result.City = "Неизвестно"
	}
	if result.Country == "" {
		result.Country = "—"
	}
	return result, time.Time{}, true
}

func persistTelemtUserSnapshot(users []telemtUserStats) error {
	db := database.GetDB()
	if db == nil {
		return nil
	}

	var existing []model.TelemtUserHistory
	if err := db.Find(&existing).Error; err != nil {
		return fmt.Errorf("telemt: read user history: %w", err)
	}
	byUsername := make(map[string]model.TelemtUserHistory, len(existing))
	for _, row := range existing {
		byUsername[row.Username] = row
	}

	return db.Transaction(func(tx *gorm.DB) error {
		now := time.Now().UnixMilli()

		for _, user := range users {
			if user.Username == "" {
				continue
			}

			row, exists := byUsername[user.Username]
			active := user.CurrentConnections > 0 || len(user.ActiveUniqueIPs) > 0
			if !exists && !active && user.TotalOctets == 0 {
				continue
			}
			if !exists {
				row = model.TelemtUserHistory{
					Username:    user.Username,
					FirstSeenAt: now,
				}
			}

			row.ActiveIPs = mustMarshalTelemtIPs(user.ActiveUniqueIPs)
			row.RecentIPs = mustMarshalTelemtIPs(user.RecentUniqueIPs)
			row.CurrentConnections = user.CurrentConnections

			if !exists {
				row.TotalBytes = user.TotalOctets
			} else if user.TotalOctets >= row.SourceTotalBytes {
				row.TotalBytes += user.TotalOctets - row.SourceTotalBytes
			} else {
				// Telemt restarted or reset its runtime counter. Keep the
				// panel's cumulative total and start a new source baseline.
				row.TotalBytes += user.TotalOctets
			}
			row.SourceTotalBytes = user.TotalOctets

			if row.FirstSeenAt == 0 {
				row.FirstSeenAt = now
			}
			if active {
				row.LastSeenAt = now
			}

			if err := tx.Save(&row).Error; err != nil {
				return fmt.Errorf("telemt: save user history %s: %w", user.Username, err)
			}
		}
		return nil
	})
}

func decodeTelemtIPs(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var ips []string
	if err := json.Unmarshal([]byte(raw), &ips); err != nil {
		return nil
	}
	return normalizeTelemtIPs(ips)
}

func mustMarshalTelemtIPs(ips []string) string {
	data, _ := json.Marshal(normalizeTelemtIPs(ips))
	return string(data)
}

func normalizeTelemtIPs(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		ip := normalizeTelemtIP(value)
		if ip == "" {
			continue
		}
		if _, ok := seen[ip]; ok {
			continue
		}
		seen[ip] = struct{}{}
		out = append(out, ip)
	}
	sort.Strings(out)
	return out
}

func normalizeTelemtIP(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if ip := net.ParseIP(raw); ip != nil {
		return ip.String()
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		if ip := net.ParseIP(host); ip != nil {
			return ip.String()
		}
	}
	return ""
}

func maxInt64(value, minValue int64) int64 {
	if value < minValue {
		return minValue
	}
	return value
}
