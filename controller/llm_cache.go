package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/songquanpeng/one-api/model"
)

// GetLLMCacheStats 获取缓存统计
func GetLLMCacheStats(c *gin.Context) {
	stats := model.GetLLMCacheStats()

	// 计算命中率
	var hitRate float64
	if stats.Hits+stats.Misses > 0 {
		hitRate = float64(stats.Hits) / float64(stats.Hits+stats.Misses) * 100
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"stats":    stats,
			"hit_rate": hitRate,
		},
	})
}

// ClearLLMCache 清空缓存
func ClearLLMCache(c *gin.Context) {
	model.ClearLLMCache()

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "缓存已清空",
	})
}

// GetLLMCacheConfig 获取缓存配置
func GetLLMCacheConfig(c *gin.Context) {
	config := model.GetLLMCacheConfig()

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    config,
	})
}

// UpdateLLMCacheConfig 更新缓存配置
func UpdateLLMCacheConfig(c *gin.Context) {
	var config model.LLMCacheConfig
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "无效的配置参数",
		})
		return
	}

	model.UpdateLLMCacheConfig(config)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "配置已更新",
	})
}

// GetLLMCacheItems 获取缓存项列表
func GetLLMCacheItems(c *gin.Context) {
	items := model.GetLLMCacheItems()

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    items,
	})
}

// DeleteLLMCacheItem 删除指定缓存项
func DeleteLLMCacheItem(c *gin.Context) {
	key := c.Param("key")
	if key == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "缓存键不能为空",
		})
		return
	}

	model.DeleteLLMCacheItem(key)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "缓存项已删除",
	})
}
