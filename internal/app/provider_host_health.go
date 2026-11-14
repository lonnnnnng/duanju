package app

import (
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// providerHostHealth 记录站源/CDN 主机的实际请求结果，用于排序候选地址。
// long: 只做被动健康统计，不额外发探测请求，避免触发上游限流。
type providerHostHealth struct {
	mu    sync.Mutex
	hosts map[string]*providerHostStat
	now   func() time.Time
}

type providerHostStat struct {
	success      int
	failure      int
	latency      time.Duration
	failingUntil time.Time
}

const (
	providerHostFailureCooldown = 2 * time.Minute
	providerHostLatencyWeight   = 0.3
	// providerHostUnknownLatency 是未观测主机的排位基准，优先给已验证过的主机。
	providerHostUnknownLatency = 800 * time.Millisecond
)

func newProviderHostHealth() *providerHostHealth {
	return &providerHostHealth{hosts: map[string]*providerHostStat{}, now: time.Now}
}

func providerHostKey(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

// report 记录一次真实请求结果；失败主机在冷却期内会被排到候选末尾。
func (health *providerHostHealth) report(rawURL string, ok bool, elapsed time.Duration) {
	if health == nil {
		return
	}
	host := providerHostKey(rawURL)
	if host == "" {
		return
	}
	health.mu.Lock()
	defer health.mu.Unlock()
	stat := health.hosts[host]
	if stat == nil {
		stat = &providerHostStat{}
		health.hosts[host] = stat
	}
	if !ok {
		stat.failure++
		stat.failingUntil = health.now().Add(providerHostFailureCooldown)
		return
	}
	stat.success++
	stat.failingUntil = time.Time{}
	if elapsed > 0 {
		if stat.latency == 0 {
			stat.latency = elapsed
		} else {
			stat.latency = time.Duration(float64(stat.latency)*(1-providerHostLatencyWeight) + float64(elapsed)*providerHostLatencyWeight)
		}
	}
}

// score 返回 (是否处于失败冷却, 观测延迟)，未知主机给中性延迟。
func (health *providerHostHealth) score(rawURL string) (bool, time.Duration) {
	if health == nil {
		return false, providerHostUnknownLatency
	}
	host := providerHostKey(rawURL)
	if host == "" {
		return false, providerHostUnknownLatency
	}
	health.mu.Lock()
	defer health.mu.Unlock()
	stat := health.hosts[host]
	if stat == nil {
		return false, providerHostUnknownLatency
	}
	cooldown := !stat.failingUntil.IsZero() && health.now().Before(stat.failingUntil)
	latency := stat.latency
	if latency <= 0 {
		latency = providerHostUnknownLatency
	}
	return cooldown, latency
}

// order 按健康度稳定排序：未失败优先，其次延迟低者优先，最后保持原有顺序。
func (health *providerHostHealth) order(urls []string) []string {
	if health == nil || len(urls) < 2 {
		return urls
	}
	type ranked struct {
		value    string
		index    int
		cooldown bool
		latency  time.Duration
	}
	items := make([]ranked, 0, len(urls))
	for index, value := range urls {
		cooldown, latency := health.score(value)
		items = append(items, ranked{value: value, index: index, cooldown: cooldown, latency: latency})
	}
	sort.SliceStable(items, func(left, right int) bool {
		if items[left].cooldown != items[right].cooldown {
			return !items[left].cooldown
		}
		if items[left].latency != items[right].latency {
			return items[left].latency < items[right].latency
		}
		return items[left].index < items[right].index
	})
	ordered := make([]string, 0, len(items))
	for _, item := range items {
		ordered = append(ordered, item.value)
	}
	return ordered
}

// orderMediaAddresses 把同一清晰度的多个备用地址按健康度排序，只保留有效地址。
func (health *providerHostHealth) orderMediaAddresses(addresses []string) []string {
	if len(addresses) < 2 {
		return addresses
	}
	unique := make([]string, 0, len(addresses))
	seen := map[string]bool{}
	for _, address := range addresses {
		if address == "" || seen[address] {
			continue
		}
		seen[address] = true
		unique = append(unique, address)
	}
	return health.order(unique)
}
