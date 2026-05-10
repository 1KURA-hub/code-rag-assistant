package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port                string
	PostgresDSN         string
	WorkDir             string
	EmbeddingDim        int
	EmbeddingBatchSize  int
	ChunkMaxLines       int
	ChunkOverlapLines   int
	TopK                int
	PromptCitationLimit int
	PromptChunkMaxChars int
	MaxRepoBytes        int64
	GitHubTimeout       time.Duration
	GitHubProxyURL      string
	LLMTimeout          time.Duration
	RedisAddr           string
	RedisPassword       string
	RedisDB             int
	RepoCacheTTL        time.Duration
	OpenAIBaseURL       string
	OpenAIAPIKey        string
	OpenAIModel         string
	EmbeddingModel      string
	EmbeddingProvider   string
}

func Load() Config {
	openAIAPIKey := getenv("OPENAI_API_KEY", os.Getenv("DASHSCOPE_API_KEY"))
	openAIBaseURL := getenv("OPENAI_BASE_URL", "https://api.openai.com/v1")
	if os.Getenv("OPENAI_BASE_URL") == "" && os.Getenv("OPENAI_API_KEY") == "" && os.Getenv("DASHSCOPE_API_KEY") != "" {
		openAIBaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	}

	return Config{
		Port:                getenv("PORT", "8090"),
		PostgresDSN:         getenv("POSTGRES_DSN", "host=127.0.0.1 user=code_rag password=code_rag dbname=code_rag port=5432 sslmode=disable"),
		WorkDir:             getenv("WORK_DIR", "./tmp/repos"),
		EmbeddingDim:        getenvInt("EMBEDDING_DIM", 128),
		EmbeddingBatchSize:  getenvInt("EMBEDDING_BATCH_SIZE", 10),
		ChunkMaxLines:       getenvInt("CHUNK_MAX_LINES", 80),
		ChunkOverlapLines:   getenvInt("CHUNK_OVERLAP_LINES", 12),
		TopK:                getenvInt("TOP_K", 8),
		PromptCitationLimit: getenvInt("PROMPT_CITATION_LIMIT", 4),
		PromptChunkMaxChars: getenvInt("PROMPT_CHUNK_MAX_CHARS", 1200),
		MaxRepoBytes:        int64(getenvInt("MAX_REPO_MB", 30)) * 1024 * 1024,
		GitHubTimeout:       time.Duration(getenvInt("GITHUB_TIMEOUT_SECONDS", 30)) * time.Second,
		GitHubProxyURL:      strings.TrimRight(getenv("GITHUB_PROXY_URL", ""), "/"),
		LLMTimeout:          time.Duration(getenvInt("LLM_TIMEOUT_SECONDS", 60)) * time.Second,
		RedisAddr:           getenv("REDIS_ADDR", ""),
		RedisPassword:       getenv("REDIS_PASSWORD", ""),
		RedisDB:             getenvInt("REDIS_DB", 0),
		RepoCacheTTL:        time.Duration(getenvInt("REPO_CACHE_TTL_SECONDS", 10)) * time.Second,
		OpenAIBaseURL:       openAIBaseURL,
		OpenAIAPIKey:        openAIAPIKey,
		OpenAIModel:         getenv("OPENAI_MODEL", "gpt-4o-mini"),
		EmbeddingModel:      getenv("EMBEDDING_MODEL", "text-embedding-3-small"),
		EmbeddingProvider:   strings.ToLower(getenv("EMBEDDING_PROVIDER", "remote")),
	}
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
