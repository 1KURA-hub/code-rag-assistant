package service

import "strings"

var queryAliases = []struct {
	Terms   []string
	Aliases []string
}{
	{
		Terms: []string{"项目", "代码", "仓库", "结构", "架构", "模块", "目录", "分层", "主流程", "调用链"},
		Aliases: []string{
			"project", "code", "repository", "repo", "module", "package",
			"architecture", "structure", "layer", "service", "dependency", "flow",
		},
	},
	{
		Terms: []string{"函数", "方法", "结构体", "类型", "常量", "变量", "声明", "分片", "切片", "ast", "语法树", "行号"},
		Aliases: []string{
			"function", "method", "struct", "interface", "type", "const", "var",
			"declaration", "symbol", "chunk", "parser", "parse", "ast", "line",
		},
	},
	{
		Terms: []string{"接口", "api", "路由", "handler", "controller", "endpoint", "参数", "请求", "响应", "绑定"},
		Aliases: []string{
			"api", "router", "route", "routes", "handler", "controller", "endpoint",
			"request", "response", "param", "params", "binding", "bind", "json",
		},
	},
	{
		Terms: []string{"数据库", "表", "模型", "落库", "查询", "事务", "索引", "mysql", "postgres", "gorm", "dao"},
		Aliases: []string{
			"database", "db", "mysql", "postgres", "postgresql", "table", "model",
			"schema", "repository", "dao", "gorm", "query", "transaction", "commit",
			"rollback", "migration", "index",
		},
	},
	{
		Terms: []string{"缓存", "redis", "热点", "穿透", "击穿", "过期", "ttl", "布隆", "singleflight"},
		Aliases: []string{
			"redis", "cache", "key", "ttl", "expire", "expiration", "bloom",
			"filter", "singleflight", "hot key", "penetration", "breakdown",
		},
	},
	{
		Terms: []string{"消息", "队列", "mq", "异步", "生产者", "消费者", "消费", "重试", "死信", "ack", "nack"},
		Aliases: []string{
			"message", "queue", "mq", "producer", "consumer", "consume",
			"async", "retry", "ack", "nack", "dead letter", "dlq", "routing",
		},
	},
	{
		Terms: []string{"登录", "注册", "认证", "鉴权", "权限", "token", "jwt", "session", "中间件"},
		Aliases: []string{
			"auth", "authentication", "authorization", "login", "register",
			"token", "jwt", "session", "middleware", "claims", "permission",
		},
	},
	{
		Terms: []string{"配置", "环境变量", "启动", "初始化", "依赖", "部署", "docker", "compose", "yaml", "json"},
		Aliases: []string{
			"config", "configuration", "env", "environment", "startup",
			"bootstrap", "init", "main", "dependency", "docker", "compose",
			"deploy", "yaml", "json",
		},
	},
	{
		Terms: []string{"并发", "协程", "goroutine", "channel", "锁", "超时", "context", "worker", "线程池"},
		Aliases: []string{
			"concurrency", "concurrent", "goroutine", "channel", "context",
			"timeout", "deadline", "cancel", "lock", "mutex", "worker", "pool",
		},
	},
	{
		Terms: []string{"一致性", "幂等", "重复", "去重", "事务", "回滚", "唯一索引", "补偿"},
		Aliases: []string{
			"consistency", "idempotency", "idempotent", "duplicate", "dedup",
			"unique", "unique index", "transaction", "commit", "rollback", "compensation",
		},
	},
	{
		Terms: []string{"错误", "异常", "失败", "降级", "兜底", "日志", "监控", "排查", "debug"},
		Aliases: []string{
			"error", "exception", "failure", "fallback", "degrade", "log",
			"logger", "metrics", "trace", "debug", "monitor",
		},
	},
	{
		Terms: []string{"测试", "单元测试", "集成测试", "mock", "覆盖率", "压测", "benchmark"},
		Aliases: []string{
			"test", "tests", "unit test", "integration test", "mock", "assert",
			"coverage", "benchmark", "load test",
		},
	},
	{
		Terms: []string{"前端", "页面", "组件", "状态", "props", "hook", "样式", "css", "react", "vue"},
		Aliases: []string{
			"frontend", "page", "component", "state", "props", "hook",
			"style", "css", "react", "vue", "render",
		},
	},
	{
		Terms: []string{"ai", "rag", "embedding", "向量", "检索", "召回", "大模型", "prompt", "依据", "引用"},
		Aliases: []string{
			"ai", "rag", "embedding", "vector", "retrieval", "retrieve",
			"search", "llm", "prompt", "context", "citation", "evidence",
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
	seen := map[string]struct{}{}
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
			if _, ok := seen[alias]; !ok {
				seen[alias] = struct{}{}
				aliases = append(aliases, alias)
			}
		}
	}
	return aliases
}
