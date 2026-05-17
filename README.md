# 代码仓库 RAG 助手

面向 GitHub 代码仓库理解的 RAG 工具。导入公开仓库后，系统会下载源码、按代码结构分片、生成 embedding，之后支持代码问答和 git diff 影响分析，并返回可追溯的文件路径、行号、符号名和源码片段。

这个项目不是通用知识库问答，重点解决代码场景里的两个问题：

- 大模型直接解释代码容易缺少真实依据。
- 只靠向量相似度时，文件名、函数名、配置名等精确信息命中不稳定。

## 核心能力

- **异步仓库索引**：创建仓库任务后立即返回，下载、扫描、分片、embedding 和入库在后台执行。
- **Go AST 语义分片**：Go 文件优先按函数、方法、类型声明切分，解析失败时降级为固定行窗口。
- **混合检索**：pgvector 向量召回 + 字段关键词召回 + PostgreSQL full-text search。
- **RRF 融合重排**：多路召回命中的同一代码片段会获得更高排序。
- **流式回答**：`/api/ask/stream` 支持边生成边返回，前端逐段展示。
- **代码依据展示**：回答附带 citations，包含文件路径、行号、语言、符号和源码内容。
- **diff 影响分析**：根据 diff 召回相关代码，输出影响模块、风险点和建议测试。
- **Redis 状态缓存**：缓存仓库状态，减少前端轮询对 PostgreSQL 的压力。

当前线上样例仓库 `course-select`：

```text
扫描文件数：47
代码分片数：164
索引耗时：约 9.2s
```

## 技术栈

```text
Go / Gin / GORM
PostgreSQL / pgvector / Redis
React / Vite
Docker Compose
OpenAI-compatible Chat + Embedding API
```

## 核心链路

### 仓库索引

```text
GitHub URL
-> 下载默认分支 ZIP
-> 扫描源码文件
-> Go AST 分片 / 行窗口分片
-> 批量生成 embedding
-> 写入 code_chunks
-> 仓库状态更新为 ready
```

### 代码问答

```text
用户问题
-> 提取路径、符号、语言和关键词
-> 可选 Query Rewrite
-> 问题 embedding
-> 向量检索与关键词检索并行执行
-> RRF 融合重排
-> 组织 prompt
-> JSON 或 NDJSON 流式返回
```

### 变更影响分析

```text
git diff
-> 提取变更文件和关键词
-> 检索相关代码片段
-> 生成变更总结、影响模块、风险点和建议测试
```

## 快速启动

```bash
docker compose up -d --build
```

访问：

```text
http://127.0.0.1:8090
```

本地只运行 Go 服务：

```bash
docker compose up -d postgres redis

export POSTGRES_DSN="host=127.0.0.1 user=code_rag password=code_rag dbname=code_rag port=5433 sslmode=disable"
export REDIS_ADDR="127.0.0.1:6379"

go run ./cmd/server
```

常用配置：

```bash
export OPENAI_API_KEY="..."
export OPENAI_BASE_URL="https://api.openai.com/v1"
export OPENAI_MODEL="gpt-4o-mini"
export EMBEDDING_MODEL="text-embedding-3-small"
export EMBEDDING_PROVIDER="remote"
```

未配置 API Key 时，embedding 会使用本地 hash fallback，便于跑通流程，但检索质量不等同于真实 embedding 模型。

## CI/CD

仓库内置 GitHub Actions：

- Pull Request 和推送会自动执行测试与 Docker 镜像构建
- `main` 分支推送通过后，会自动 SSH 到云服务器更新服务并检查 `/healthz`

需要在 GitHub 仓库的 Actions Secrets 中配置：

```text
DEPLOY_HOST
DEPLOY_USER
DEPLOY_SSH_KEY
DEPLOY_PORT  # 可选，默认 22
```

## API

### 仓库

```http
POST /api/repos
GET  /api/repos/:id
```

创建仓库请求：

```json
{"repo_url":"https://github.com/owner/repo"}
```

仓库状态：

```text
pending  -> 等待后台索引
indexing -> 正在索引
ready    -> 可以问答
failed   -> 索引失败
```

### 代码问答

```http
POST /api/ask
POST /api/ask/stream
```

请求体：

```json
{"repository_id":1,"question":"回答链路是怎么生成答案的？"}
```

`/api/ask` 返回完整 JSON：

```json
{"answer":"...","citations":[]}
```

`/api/ask/stream` 返回 `application/x-ndjson`：

```json
{"type":"citations","citations":[]}
{"type":"delta","delta":"回答内容"}
{"type":"done","answer":"完整回答"}
```

前端默认优先使用流式接口，失败时回退到 `/api/ask`。

### 变更影响分析

```http
POST /api/impact
```

请求体：

```json
{"repository_id":1,"diff_text":"diff --git ..."}
```

返回变更总结、影响模块、风险点、建议测试和相关代码依据。

## 离线检索评估

```bash
go run ./cmd/retrieval-eval --repo-id=1
```

评估集位于 `internal/service/testdata/`，用于比较检索策略调整前后的 HitRate、Recall 和 MRR。

## 当前边界

- 只支持 GitHub 公开仓库默认分支 ZIP 导入。
- 不支持私有仓库 OAuth、分支选择和增量索引。
- 索引任务是进程内 goroutine，不是持久化任务队列。
- 没有多用户权限隔离和仓库归属管理。
- 检索使用 RRF 融合，没有接入独立 reranker 模型。
- 项目定位是代码理解和影响分析，不负责自动修改代码。
