# 代码仓库 RAG 助手

一个面向 GitHub 代码仓库理解的 RAG 工具。导入仓库后，系统会扫描源码、按代码结构分片、生成向量索引，并在问答或 git diff 影响分析时返回带文件路径、行号和源码片段的代码依据。

这个项目关注的不是通用企业知识库问答，而是代码场景里的两个问题：

- 大模型直接解释代码时容易缺少可追溯依据。
- 只靠向量相似度检索时，文件名、函数名、配置名等精确信息命中不稳定。

因此项目在 `pgvector` 向量召回基础上加入关键词召回、PostgreSQL 全文检索和 RRF 融合重排，让回答尽量回到真实代码片段。

## 项目亮点

- **异步仓库索引**：导入仓库后立即返回任务状态，下载、解压、分片、embedding 和入库在后台执行。
- **Go AST 语义分片**：Go 文件优先按函数、方法、类型声明切分，减少固定行数切分造成的上下文截断。
- **PostgreSQL + pgvector**：使用一个 PostgreSQL 数据库存储仓库记录、代码片段、全文检索向量和 embedding 向量。
- **混合检索与 RRF 重排**：结合向量召回、字段关键词召回和 full-text search，提高文件名、函数名和代码关键词问题的命中率。
- **带 citation 的回答**：问答结果返回文件路径、起止行号、符号名和源码内容，前端可展示 RAG Evidence。
- **diff 影响分析**：根据 git diff 提取变更路径和关键词，召回相关代码上下文，输出影响模块、风险点和建议测试。
- **索引状态缓存**：使用 Redis Cache Aside 缓存仓库状态，降低前端轮询对 PostgreSQL 的访问压力。

## 当前效果

以 `course-select` 仓库为测试对象：

```text
扫描文件数：42
代码分片数：205
批量 embedding 后索引耗时：约 4.9s
离线评估用例：51 条
HitRate@5：约 84.3%
Recall@5：约 0.794
```

说明：离线评估集是小规模本地回归集，主要用于比较检索策略改动前后的效果，不等同于大规模线上准确率评测。

## 技术栈

```text
Golang / Gin / GORM
PostgreSQL / pgvector / Redis
Docker / GitHub ZIP / OpenAI-compatible API
```

## 核心链路

### 仓库索引链路

```text
GitHub URL
-> 解析 owner/repo
-> 创建或更新 repositories 记录
-> 后台 goroutine 下载默认分支 ZIP
-> 解压并扫描目标文件
-> Go AST 分片 / 普通行窗口分片
-> 构造 embedding 文本
-> 批量调用 embedding 模型生成向量
-> 写入 code_chunks
-> 更新仓库状态为 ready
```

### 代码问答链路

```text
用户问题
-> 提取文件路径、函数名、语言和普通关键词
-> 问题 embedding
-> pgvector 向量召回候选代码片段
-> 字段关键词召回和 PostgreSQL 全文检索
-> RRF 融合重排
-> 组织 prompt
-> 调用大模型生成回答
-> 返回 answer + citations
```

### 变更影响分析链路

```text
git diff
-> 提取变更文件和关键词
-> 检索相关代码片段
-> 组织变更上下文
-> 输出变更总结、影响模块、风险点和建议测试
-> 返回 citations
```

## 后端设计

### 数据模型

项目核心模型是 `Repository` 和 `CodeChunk`：

```text
repositories 1 : N code_chunks
```

`repositories` 保存仓库 URL、owner、name、索引状态、文件数、分片数、索引耗时和失败原因。  
`code_chunks` 保存文件路径、起止行号、语言、符号名、符号类型、源码内容、全文检索向量和 embedding 向量。

问答和影响分析都会使用 `repository_id` 限定检索范围，避免不同仓库的代码片段互相干扰。

### 代码分片

Go 文件使用 `go/parser` 和 `go/ast` 解析源码结构，优先按函数、方法和类型声明生成分片。分片会保留源码内容、文件路径、起止行号、符号名和符号类型。

如果 Go 文件解析失败，会降级为普通行窗口分片，避免单个异常文件导致整个仓库索引失败。非 Go 文件使用固定行窗口切分。

### embedding 文本

系统不会只把源码原文送去 embedding，而是构造包含元信息的文本：

```text
file: internal/service/ingest.go
symbol: IngestService.CreateAndIndex
type: function
language: go
content:
...
```

这样可以让向量表示同时包含代码语义、文件路径和符号信息。`embeddingText` 只是生成向量时的临时文本，不单独持久化；真正持久化的是代码分片字段和 `embedding_vector`。

### 检索与重排

检索由三部分组成：

```text
向量召回：根据问题 embedding 使用 pgvector 余弦距离召回语义相关片段
关键词召回：按 file_path / symbol_name / language / content 字段查精确线索
全文检索：使用 PostgreSQL tsvector 查询普通代码关键词
```

多路召回结果通过 RRF 融合重排。同一个分片如果同时被向量检索和关键词检索命中，最终排序会更靠前。

### 异步索引和状态保护

仓库索引耗时较长，创建仓库接口不会等待索引完成，而是返回仓库记录并由后台 goroutine 执行索引。前端通过状态查询接口轮询结果。

状态流转：

```text
pending  -> 任务已创建，等待后台执行
indexing -> 正在下载、分片、向量化和入库
ready    -> 索引完成，可以问答
failed   -> 首次索引失败，没有可用分片
```

同一个仓库处于 `pending` 或 `indexing` 时，重复提交不会启动新的索引任务。为了避免服务重启后状态卡死，`pending/indexing` 超过 10 分钟未更新时允许重新提交索引。

如果重新索引失败，但数据库中仍有旧分片，系统会保持 `ready`，并在 `error_message` 中记录失败原因，继续使用旧索引提供问答能力。

### Redis 缓存

前端会轮询仓库状态，因此仓库状态查询使用 Redis 做 Cache Aside：

```text
查询状态 -> 先读 Redis -> 未命中查 PostgreSQL -> 回写 Redis
状态变更 -> 删除缓存 -> 下次查询重新加载最新状态
```

这里选择状态更新后删除缓存，而不是直接更新缓存，目的是让 PostgreSQL 作为最终数据源，减少缓存与数据库状态不一致的风险。

### 离线检索评估

项目提供小规模离线评估命令，用于验证检索链路调整是否有效。评估集位于：

```text
internal/service/testdata/retrieval_eval_cases.json
```

运行：

```bash
go run ./cmd/retrieval-eval --repo-id=1
```

命令会调用真实的 `Retriever.Search`，输出：

```text
HitRate@1
HitRate@3
HitRate@5
Recall@5
MRR
```

并按 `chat`、`repo`、`chunking`、`retrieval`、`impact` 等类型输出分类指标。

## 快速启动

使用 Docker 启动 PostgreSQL、Redis 和应用：

```bash
docker compose up -d --build
```

打开：

```text
http://127.0.0.1:8090
```

如果只想本地运行 Go 服务，可以先启动依赖：

```bash
docker compose up -d postgres redis

export POSTGRES_DSN="host=127.0.0.1 user=code_rag password=code_rag dbname=code_rag port=5433 sslmode=disable"
export REDIS_ADDR="127.0.0.1:6379"

go run ./cmd/server
```

可选的大模型配置：

```bash
export OPENAI_API_KEY="..."
export OPENAI_BASE_URL="https://api.openai.com/v1"
export OPENAI_MODEL="gpt-4o-mini"
export EMBEDDING_MODEL="text-embedding-3-small"
export EMBEDDING_PROVIDER="remote"
export EMBEDDING_BATCH_SIZE="10"
export PROMPT_CITATION_LIMIT="5"
export PROMPT_CHUNK_MAX_CHARS="1200"
export GITHUB_PROXY_URL=""
```

也可以使用 OpenAI 兼容接口，例如 DashScope、OpenRouter 等。未配置 API Key 时，项目会使用本地 hash embedding fallback，便于本地跑通流程，但检索质量不等同于真实 embedding 模型。

`PROMPT_CITATION_LIMIT` 和 `PROMPT_CHUNK_MAX_CHARS` 用来限制传给大模型的代码依据数量和单个片段长度，降低长回答的等待时间；前端代码依据栏仍展示完整检索结果。

如果服务器访问 GitHub ZIP 超时，可以配置 GitHub 代理，例如：

```bash
export GITHUB_PROXY_URL="https://gh-proxy.com"
```

## API 示例

### 创建或重新索引仓库

```http
POST /api/repos
Content-Type: application/json

{
  "repo_url": "https://github.com/owner/repo"
}
```

同一个 `owner/repo` 重复导入时会复用仓库记录。若当前仓库正在 `pending/indexing`，接口会直接返回当前任务，不重复启动索引。

### 查询仓库状态

```http
GET /api/repos/:id
```

示例响应：

```json
{
  "id": 1,
  "repo_url": "https://github.com/owner/repo",
  "owner": "owner",
  "name": "repo",
  "status": "ready",
  "error_message": "",
  "file_count": 42,
  "chunk_count": 205,
  "index_duration_ms": 4900,
  "indexed_at": "2026-04-27T12:00:00Z"
}
```

### 代码问答

```http
POST /api/ask
Content-Type: application/json

{
  "repository_id": 1,
  "question": "这个项目的仓库索引流程是什么？"
}
```

返回结果包含回答正文和 citations。每个 citation 会带上文件路径、起止行号、语言、符号名、符号类型、源码内容和检索分数。

### 变更影响分析

```http
POST /api/impact
Content-Type: application/json

{
  "repository_id": 1,
  "diff_text": "diff --git ..."
}
```

返回结果包含变更总结、影响模块、风险点、建议测试和相关代码依据。

## 当前边界

- 只支持 GitHub 公开仓库和默认分支 ZIP 导入。
- 不支持私有仓库 OAuth、分支选择和增量索引。
- 索引任务目前是进程内 goroutine，不是持久化任务队列。
- 没有多用户权限隔离和仓库归属管理。
- 检索使用 RRF 规则融合，没有接入专门的 reranker 模型。
- 不支持自动修改代码，定位是代码理解和影响分析工具。
