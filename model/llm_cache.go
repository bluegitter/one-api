package model

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/songquanpeng/one-api/common"
	"github.com/songquanpeng/one-api/common/logger"
	relaymodel "github.com/songquanpeng/one-api/relay/model"
)

// LLM Cache 配置变量
var (
	LLMCacheEnabled             = true
	LLMCacheTTL                 = 3600 // 1小时
	LLMCacheMaxSize             = 10000
	LLMCacheMinResponseLength   = 10
	LLMCacheMaxResponseLength   = 10000
	LLMCacheSimilarityThreshold = 0.95
)

// LLMCacheItem 缓存项结构
type LLMCacheItem struct {
	RequestHash   string                   `json:"request_hash"`
	Model         string                   `json:"model"`
	Response      *relaymodel.TextResponse `json:"response"`
	Usage         *relaymodel.Usage        `json:"usage"`
	CreatedAt     int64                    `json:"created_at"`
	ExpiresAt     int64                    `json:"expires_at"`
	HitCount      int64                    `json:"hit_count"`
	LastAccessed  int64                    `json:"last_accessed"`
	RequestParams map[string]interface{}   `json:"request_params"`
}

// LLMCacheStats 缓存统计
type LLMCacheStats struct {
	TotalItems int64 `json:"total_items"`
	Hits       int64 `json:"hits"`
	Misses     int64 `json:"misses"`
	Evictions  int64 `json:"evictions"`
	TotalSize  int64 `json:"total_size"`
	MaxSize    int64 `json:"max_size"`
}

// LLMCacheConfig 缓存配置
type LLMCacheConfig struct {
	Enabled             bool    `json:"enabled"`
	TTL                 int64   `json:"ttl"`
	MaxSize             int     `json:"max_size"`
	MinResponseLength   int     `json:"min_response_length"`
	MaxResponseLength   int     `json:"max_response_length"`
	SimilarityThreshold float64 `json:"similarity_threshold"`
}

var (
	llmCache       = make(map[string]*LLMCacheItem)
	llmCacheMutex  sync.RWMutex
	llmCacheStats  = &LLMCacheStats{}
	llmCacheConfig = LLMCacheConfig{
		Enabled:             false, // 默认禁用，通过InitLLMCache设置
		TTL:                 3600,  // 1小时
		MaxSize:             10000,
		MinResponseLength:   10,
		MaxResponseLength:   10000,
		SimilarityThreshold: 0.95,
	}
)

// InitLLMCache 初始化LLM缓存
func InitLLMCache() {
	if !LLMCacheEnabled {
		logger.SysLog("LLM cache disabled")
		return
	}

	logger.SysLog("LLM cache initialized")
	logger.SysLog(fmt.Sprintf("LLM cache config: TTL=%d, MaxSize=%d, MinLength=%d, MaxLength=%d, Threshold=%.2f",
		LLMCacheTTL, LLMCacheMaxSize, LLMCacheMinResponseLength,
		LLMCacheMaxResponseLength, LLMCacheSimilarityThreshold))

	// 启动清理过期缓存的goroutine
	go cleanExpiredCache()
}

// GenerateCacheKey 生成缓存键
func GenerateCacheKey(request *relaymodel.GeneralOpenAIRequest) string {
	// 创建用于哈希的数据结构
	cacheData := map[string]interface{}{
		"model":             request.Model,
		"messages":          request.Messages,
		"max_tokens":        request.MaxTokens,
		"temperature":       request.Temperature,
		"top_p":             request.TopP,
		"frequency_penalty": request.FrequencyPenalty,
		"presence_penalty":  request.PresencePenalty,
		"stream":            request.Stream,
	}

	// 序列化为JSON
	jsonData, err := json.Marshal(cacheData)
	if err != nil {
		logger.Errorf(context.Background(), "failed to marshal cache data: %s", err.Error())
		return ""
	}

	// 生成SHA256哈希
	hash := sha256.Sum256(jsonData)
	return fmt.Sprintf("llm_cache:%x", hash)
}

// GetLLMCache 获取缓存项
func GetLLMCache(key string) (*LLMCacheItem, bool) {
	if !LLMCacheEnabled {
		return nil, false
	}

	logger.Infof(context.Background(), "GetLLMCache called, key=%s, enabled=%v", key, LLMCacheEnabled)

	llmCacheMutex.RLock()
	defer llmCacheMutex.RUnlock()

	item, exists := llmCache[key]
	if !exists {
		llmCacheStats.Misses++
		return nil, false
	}

	// 检查是否过期
	if time.Now().Unix() > item.ExpiresAt {
		llmCacheMutex.RUnlock()
		llmCacheMutex.Lock()
		delete(llmCache, key)
		llmCacheStats.TotalItems--
		llmCacheMutex.Unlock()
		llmCacheMutex.RLock()
		llmCacheStats.Misses++
		return nil, false
	}

	// 更新访问统计
	item.HitCount++
	item.LastAccessed = time.Now().Unix()
	llmCacheStats.Hits++

	logger.Infof(context.Background(), "LLM cache hit: %s", key)

	return item, true
}

// SetLLMCache 设置缓存项
func SetLLMCache(key string, response *relaymodel.TextResponse, usage *relaymodel.Usage, request *relaymodel.GeneralOpenAIRequest) {
	if !LLMCacheEnabled {
		return
	}

	logger.Infof(context.Background(), "SetLLMCache called, key=%s, enabled=%v", key, LLMCacheEnabled)

	// 检查响应长度
	responseText := ""
	if len(response.Choices) > 0 {
		responseText = response.Choices[0].Message.StringContent()
	}

	if len(responseText) < LLMCacheMinResponseLength || len(responseText) > LLMCacheMaxResponseLength {
		logger.Debugf(context.Background(), "response length %d not in range [%d, %d], skipping cache",
			len(responseText), LLMCacheMinResponseLength, LLMCacheMaxResponseLength)
		return
	}

	now := time.Now().Unix()
	item := &LLMCacheItem{
		RequestHash:   key,
		Model:         request.Model,
		Response:      response,
		Usage:         usage,
		CreatedAt:     now,
		ExpiresAt:     now + int64(LLMCacheTTL),
		HitCount:      0,
		LastAccessed:  now,
		RequestParams: make(map[string]interface{}),
	}

	// 存储请求参数（用于调试）
	if request.Temperature != nil {
		item.RequestParams["temperature"] = *request.Temperature
	}
	if request.TopP != nil {
		item.RequestParams["top_p"] = *request.TopP
	}
	if request.MaxTokens != 0 {
		item.RequestParams["max_tokens"] = request.MaxTokens
	}

	llmCacheMutex.Lock()
	defer llmCacheMutex.Unlock()

	// 检查缓存大小限制
	if len(llmCache) >= LLMCacheMaxSize {
		// 执行LRU淘汰
		evictLRU()
	}

	llmCache[key] = item
	llmCacheStats.TotalItems++

	// 如果Redis可用，也存储到Redis
	if common.RedisEnabled {
		go func() {
			itemJSON, err := json.Marshal(item)
			if err == nil {
				common.RedisSetEx(key, string(itemJSON), int(LLMCacheTTL))
			}
		}()
	}

	logger.Debugf(context.Background(), "cached LLM response with key: %s", key)
}

// evictLRU 执行LRU淘汰
func evictLRU() {
	var oldestKey string
	var oldestTime int64 = time.Now().Unix()

	for key, item := range llmCache {
		if item.LastAccessed < oldestTime {
			oldestTime = item.LastAccessed
			oldestKey = key
		}
	}

	if oldestKey != "" {
		delete(llmCache, oldestKey)
		llmCacheStats.Evictions++
		logger.Debugf(context.Background(), "evicted LLM cache item: %s", oldestKey)
	}
}

// cleanExpiredCache 清理过期缓存
func cleanExpiredCache() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now().Unix()
		llmCacheMutex.Lock()

		for key, item := range llmCache {
			if now > item.ExpiresAt {
				delete(llmCache, key)
				llmCacheStats.TotalItems--
				logger.Debugf(context.Background(), "cleaned expired LLM cache item: %s", key)
			}
		}

		llmCacheMutex.Unlock()
	}
}

// GetLLMCacheStats 获取缓存统计
func GetLLMCacheStats() *LLMCacheStats {
	llmCacheMutex.RLock()
	defer llmCacheMutex.RUnlock()

	stats := *llmCacheStats
	stats.TotalItems = int64(len(llmCache))
	stats.MaxSize = int64(LLMCacheMaxSize)
	return &stats
}

// ClearLLMCache 清空缓存
func ClearLLMCache() {
	llmCacheMutex.Lock()
	defer llmCacheMutex.Unlock()

	llmCache = make(map[string]*LLMCacheItem)
	llmCacheStats = &LLMCacheStats{}

	// 如果Redis可用，也清空Redis中的缓存
	if common.RedisEnabled {
		go func() {
			keys, err := common.RedisKeys("llm_cache:*")
			if err == nil {
				for _, key := range keys {
					common.RedisDel(key)
				}
			}
		}()
	}

	logger.SysLog("LLM cache cleared")
}

// GetLLMCacheConfig 获取缓存配置
func GetLLMCacheConfig() LLMCacheConfig {
	llmCacheMutex.RLock()
	defer llmCacheMutex.RUnlock()
	return llmCacheConfig
}

// GetLLMCacheItems 获取缓存项列表（用于管理界面）
func GetLLMCacheItems() []*LLMCacheItem {
	llmCacheMutex.RLock()
	defer llmCacheMutex.RUnlock()

	items := make([]*LLMCacheItem, 0, len(llmCache))
	for _, item := range llmCache {
		items = append(items, item)
	}
	return items
}

// DeleteLLMCacheItem 删除指定缓存项
func DeleteLLMCacheItem(key string) {
	llmCacheMutex.Lock()
	defer llmCacheMutex.Unlock()

	if _, exists := llmCache[key]; exists {
		delete(llmCache, key)
		llmCacheStats.TotalItems--

		// 如果Redis可用，也从Redis删除
		if common.RedisEnabled {
			go common.RedisDel(key)
		}

		logger.Debugf(context.Background(), "LLM cache item deleted: %s", key)
	}
}

// UpdateLLMCacheConfig 更新缓存配置
func UpdateLLMCacheConfig(config LLMCacheConfig) {
	llmCacheMutex.Lock()
	defer llmCacheMutex.Unlock()

	llmCacheConfig = config

	// 如果禁用了缓存，清空现有缓存
	if !config.Enabled {
		llmCache = make(map[string]*LLMCacheItem)
		llmCacheStats = &LLMCacheStats{}
	}

	logger.SysLog("LLM cache config updated")
}
