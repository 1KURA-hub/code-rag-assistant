package service

import "strings"

var queryAliases = []struct {
	Terms   []string
	Aliases []string
}{
	{
		Terms: []string{"rabbitmq", "mq", "消息队列", "消息", "消费", "消费者", "重试", "死信", "ack", "nack"},
		Aliases: []string{
			"rabbitmq", "rabbit", "mq", "message", "consumer", "consume", "delivery",
			"ack", "nack", "retry", "dead", "deadletter", "dlq", "queue", "exchange",
		},
	},
	{
		Terms: []string{"redis stream", "stream", "转发", "pending", "消费者组", "reclaim", "relay"},
		Aliases: []string{
			"redis", "stream", "relay", "xreadgroup", "xgroup", "xadd", "xack",
			"xautoclaim", "pending", "consumer", "group", "reclaim",
		},
	},
	{
		Terms: []string{"幂等", "重复", "去重", "重复请求", "重复消息"},
		Aliases: []string{
			"idempotent", "idempotency", "dedup", "duplicate", "request", "message",
			"redis", "unique", "lock",
		},
	},
	{
		Terms: []string{"选课", "课程", "库存", "扣减", "余量"},
		Aliases: []string{
			"course", "select", "selection", "enroll", "enrollment", "stock",
			"quota", "capacity", "decrement",
		},
	},
	{
		Terms: []string{"jwt", "登录", "认证", "鉴权", "token", "中间件"},
		Aliases: []string{
			"jwt", "auth", "authentication", "authorization", "token", "middleware",
			"claim", "claims",
		},
	},
	{
		Terms: []string{"lua", "脚本", "布隆", "过滤器", "bloom"},
		Aliases: []string{
			"lua", "script", "bloom", "filter", "redis", "eval", "sha",
		},
	},
}

func expandQueryText(query string, hints []string) string {
	var b strings.Builder
	b.WriteString(query)
	for _, hint := range hints {
		b.WriteByte('\n')
		b.WriteString(hint)
	}
	for _, alias := range matchedAliases(query + "\n" + strings.Join(hints, "\n")) {
		b.WriteByte('\n')
		b.WriteString(alias)
	}
	return b.String()
}

func matchedAliases(text string) []string {
	lower := strings.ToLower(text)
	seen := map[string]bool{}
	var aliases []string
	for _, group := range queryAliases {
		matched := false
		for _, term := range group.Terms {
			if strings.Contains(lower, strings.ToLower(term)) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		for _, alias := range group.Aliases {
			if !seen[alias] {
				seen[alias] = true
				aliases = append(aliases, alias)
			}
		}
	}
	return aliases
}
