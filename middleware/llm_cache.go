package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/songquanpeng/one-api/common/logger"
	"github.com/songquanpeng/one-api/model"
	relaymodel "github.com/songquanpeng/one-api/relay/model"
)

// LLMCacheMiddleware LLM缓存中间件
func LLMCacheMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 只处理非流式的聊天完成请求
		if !isCacheableRequest(c) {
			c.Next()
			return
		}

		// 获取请求体
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			logger.Errorf(c.Request.Context(), "failed to read request body: %s", err.Error())
			c.Next()
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewBuffer(body))

		// 解析请求
		var request relaymodel.GeneralOpenAIRequest
		if err := json.Unmarshal(body, &request); err != nil {
			logger.Errorf(c.Request.Context(), "failed to unmarshal request: %s", err.Error())
			c.Next()
			return
		}

		// 生成缓存键
		cacheKey := model.GenerateCacheKey(&request)
		if cacheKey == "" {
			c.Next()
			return
		}

		// 尝试从缓存获取
		if cacheItem, found := model.GetLLMCache(cacheKey); found {
			logger.Infof(c.Request.Context(), "LLM cache hit: %s", cacheKey)

			// 返回缓存的响应
			c.JSON(http.StatusOK, cacheItem.Response)
			c.Abort()
			return
		}

		// 缓存未命中，继续处理请求
		logger.Debugf(c.Request.Context(), "LLM cache miss: %s", cacheKey)

		// 创建响应写入器来捕获响应
		responseWriter := &responseCaptureWriter{
			ResponseWriter: c.Writer,
			body:           &bytes.Buffer{},
		}
		c.Writer = responseWriter

		c.Next()

		// 处理响应
		if responseWriter.statusCode == http.StatusOK {
			handleCacheResponse(c, cacheKey, &request, responseWriter.body.Bytes())
		}
	}
}

// isCacheableRequest 判断是否是可缓存的请求
func isCacheableRequest(c *gin.Context) bool {
	// 只处理POST请求
	if c.Request.Method != http.MethodPost {
		return false
	}

	// 只处理聊天完成请求
	if !strings.HasSuffix(c.Request.URL.Path, "/chat/completions") {
		return false
	}

	// 检查请求头，跳过流式请求
	contentType := c.GetHeader("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		return false
	}

	return true
}

// handleCacheResponse 处理响应并缓存
func handleCacheResponse(c *gin.Context, cacheKey string, request *relaymodel.GeneralOpenAIRequest, responseBody []byte) {
	// 解析响应
	var response relaymodel.TextResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		logger.Errorf(c.Request.Context(), "failed to unmarshal response: %s", err.Error())
		return
	}

	// 检查是否应该缓存
	if !shouldCache(&response) {
		logger.Debugf(c.Request.Context(), "response not cacheable: %s", cacheKey)
		return
	}

	// 设置缓存
	model.SetLLMCache(cacheKey, &response, &response.Usage, request)
	logger.Infof(c.Request.Context(), "LLM cache set: %s", cacheKey)
}

// shouldCache 判断是否应该缓存
func shouldCache(response *relaymodel.TextResponse) bool {
	// 检查响应长度
	if len(response.Choices) == 0 {
		return false
	}

	responseText := response.Choices[0].Message.StringContent()
	if len(responseText) < model.LLMCacheMinResponseLength || len(responseText) > model.LLMCacheMaxResponseLength {
		return false
	}

	// 检查是否包含敏感信息（简单检查）
	content := strings.ToLower(responseText)
	if strings.Contains(content, "error") ||
		strings.Contains(content, "sorry") ||
		strings.Contains(content, "cannot") {
		return false
	}

	return true
}

// responseCaptureWriter 响应捕获写入器
type responseCaptureWriter struct {
	gin.ResponseWriter
	body       *bytes.Buffer
	statusCode int
}

func (w *responseCaptureWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func (w *responseCaptureWriter) WriteString(s string) (int, error) {
	w.body.WriteString(s)
	return w.ResponseWriter.WriteString(s)
}

func (w *responseCaptureWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

// LLMCacheStatsMiddleware 缓存统计中间件
func LLMCacheStatsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 只处理统计请求
		if c.Request.URL.Path != "/api/llm_cache/stats" {
			c.Next()
			return
		}

		stats := model.GetLLMCacheStats()
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data":    stats,
		})
		c.Abort()
	}
}

// LLMCacheClearMiddleware 清空缓存中间件
func LLMCacheClearMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 只处理清空缓存请求
		if c.Request.URL.Path != "/api/llm_cache/clear" || c.Request.Method != http.MethodPost {
			c.Next()
			return
		}

		model.ClearLLMCache()
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "缓存已清空",
		})
		c.Abort()
	}
}
