package middleware

import (
    "fmt"
    "net/http"
    "sync"
    "time"

    "github.com/labstack/echo/v4"
)

const (
    // 每分钟允许的最大请求数
    maxRequestsPerMinute = 10
    // 清理间隔时间
    cleanupInterval = 5 * time.Minute
)

type ipLimiter struct {
    sync.Mutex
    requests map[string]int       // IP+分钟 -> 请求次数
    lastSeen map[string]struct{} // IP+分钟 -> 存在标记,用于清理
}

var limiter = &ipLimiter{
    requests: make(map[string]int),
    lastSeen: make(map[string]struct{}),
}

func init() {
    // 启动清理goroutine
    go func() {
        for {
            time.Sleep(cleanupInterval)
            limiter.cleanup()
        }
    }()
}

// cleanup 清理过期的请求记录
func (l *ipLimiter) cleanup() {
    l.Lock()
    defer l.Unlock()
    
    // 获取当前分钟
    now := time.Now().Truncate(time.Minute)
    
    // 遍历并清理超过1分钟的数据
    for k := range l.lastSeen {
        if minute := parseMinute(k); minute.Before(now) {
            delete(l.requests, k)
            delete(l.lastSeen, k)
        }
    }
}

// 从key中解析时间
func parseMinute(key string) time.Time {
    // key格式: IP-YYYYMMDDHHmm
    layout := "200601021504"
    t, _ := time.Parse(layout, key[len(key)-12:])
    return t
}

// RateLimit IP限流中间件
func RateLimit() echo.MiddlewareFunc {
    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {
            // 获取IP地址
            ip := c.RealIP()
            
            // 获取当前分钟
            now := time.Now().Truncate(time.Minute)
            key := fmt.Sprintf("%s-%s", ip, now.Format("200601021504"))
            
            limiter.Lock()
            // 记录最后访问时间
            limiter.lastSeen[key] = struct{}{}
            
            // 检查请求次数
            if limiter.requests[key] >= maxRequestsPerMinute {
                limiter.Unlock()
                return c.JSON(http.StatusTooManyRequests, map[string]string{
                    "error": "Too many requests, please try again later",
                })
            }
            
            // 增加请求计数
            limiter.requests[key]++
            remaining := maxRequestsPerMinute - limiter.requests[key]
            limiter.Unlock()
            
            // 设置RateLimit相关响应头
            c.Response().Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", maxRequestsPerMinute))
            c.Response().Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
            c.Response().Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", now.Add(time.Minute).Unix()))
            
            return next(c)
        }
    }
}
