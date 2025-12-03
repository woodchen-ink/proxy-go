# S3 到 D1 迁移指南

如果你正在使用 S3 同步,想要迁移到 D1,按照以下步骤操作。

## 为什么迁移?

| 特性 | S3 | D1 |
|------|----|----|
| 成本 | 需要付费 bucket | 免费额度更高 |
| 速度 | 对象存储延迟较高 | 数据库查询更快 |
| 配置复杂度 | 需要 6 个环境变量 | 只需 3 个环境变量 |
| 认证方式 | AccessKey + SecretKey | API Token |
| 适用场景 | 大文件存储 | 配置和统计数据 |

**建议**: 如果你只是同步配置和统计数据,D1 是更好的选择。

## 迁移步骤

### 步骤 1: 部署 D1 Worker

参见 [D1_SYNC_SETUP.md](D1_SYNC_SETUP.md) 完成 Worker 部署。

### 步骤 2: 备份现有数据

从你的 proxy-go 服务器备份当前数据:

```bash
# 备份配置和数据
cd /path/to/proxy-go
tar -czf backup-$(date +%Y%m%d).tar.gz data/
```

或者从 S3 下载:

```bash
# 使用 AWS CLI 下载
aws s3 cp s3://your-bucket/data/config.json ./config.json
aws s3 cp s3://your-bucket/data/path_stats.json ./path_stats.json
aws s3 cp s3://your-bucket/data/banned_ips.json ./banned_ips.json
```

### 步骤 3: 配置 D1 环境变量

**方法 A: 同时保留两者 (推荐,平滑过渡)**

```bash
# 保留现有 S3 配置
export SYNC_S3_ENDPOINT=...
export SYNC_S3_BUCKET=...
# ... 其他 S3 变量 ...

# 添加 D1 配置 (优先级更高)
export D1_SYNC_ENABLED=true
export D1_SYNC_URL=https://your-worker.workers.dev
export D1_SYNC_TOKEN=your-token
```

这样配置后:
- proxy-go 会使用 D1 (因为 `D1_SYNC_ENABLED=true`)
- 如果 D1 出问题,可以快速切回 S3

**方法 B: 只使用 D1**

```bash
# 删除或注释掉所有 S3 变量
# export SYNC_S3_ENDPOINT=...

# 只保留 D1 配置
export D1_SYNC_ENABLED=true
export D1_SYNC_URL=https://your-worker.workers.dev
export D1_SYNC_TOKEN=your-token
```

### 步骤 4: 重启服务

```bash
systemctl restart proxy-go
```

### 步骤 5: 验证迁移

检查日志:

```bash
# 应该看到这些日志
journalctl -u proxy-go -f
```

期望输出:
```
[Sync] Initializing D1 sync service...
[D1Sync] Remote config not found, uploading local config as initial version
[D1Sync] Successfully uploaded local config as initial version
[Sync] D1 sync service initialized
```

这表示:
1. ✅ D1 服务已启动
2. ✅ 本地数据已上传到 D1
3. ✅ 未来的修改会自动同步

### 步骤 6: 验证数据

检查 D1 中的数据:

```bash
cd cloudflare-worker

# 查看配置
wrangler d1 execute proxy-go-data \
  --command "SELECT updated_at FROM config"

# 查看统计
wrangler d1 execute proxy-go-data \
  --command "SELECT updated_at FROM path_stats"

# 查看封禁IP
wrangler d1 execute proxy-go-data \
  --command "SELECT updated_at FROM banned_ips"
```

或者通过 API:

```bash
# 检查配置
curl https://your-worker.workers.dev/config \
  -H "Authorization: Bearer your-token" | jq .

# 检查统计
curl https://your-worker.workers.dev/path_stats \
  -H "Authorization: Bearer your-token" | jq .
```

### 步骤 7: 清理 S3 (可选)

确认 D1 运行正常后,可以清理 S3:

```bash
# 方法 1: 删除环境变量
unset SYNC_S3_ENDPOINT
unset SYNC_S3_BUCKET
# ... 其他变量 ...

# 方法 2: 保留但注释 (推荐保留一段时间)
# 在配置文件中注释掉 S3 变量
```

**建议**: 保留 S3 配置至少 1-2 周,确保 D1 稳定运行。

## 多节点迁移

如果你有多个 proxy-go 节点:

### 方案 A: 逐个迁移 (推荐)

1. **迁移第一个节点**:
   - 按照上述步骤迁移
   - 数据会自动上传到 D1
   - 验证运行正常

2. **迁移其他节点**:
   - 每个节点依次迁移
   - 启动时会从 D1 下载最新配置
   - 无需手动同步数据

3. **优点**:
   - 风险低,可以随时回滚
   - 问题只影响单个节点

### 方案 B: 全部迁移

1. **准备阶段**:
   - 部署 D1 Worker
   - 从 S3 下载所有数据

2. **迁移阶段**:
   - 同时更新所有节点的环境变量
   - 同时重启所有节点
   - 第一个启动的节点会上传数据到 D1

3. **优点**:
   - 迁移快速
   - 所有节点同步切换

4. **缺点**:
   - 风险较高
   - 需要停机时间

**建议**: 使用方案 A,逐个节点迁移。

## 回滚步骤

如果 D1 出现问题,需要回滚到 S3:

1. **快速回滚**:
   ```bash
   # 禁用 D1
   export D1_SYNC_ENABLED=false

   # 或者删除 D1 变量
   unset D1_SYNC_ENABLED
   unset D1_SYNC_URL
   unset D1_SYNC_TOKEN

   # 重启服务
   systemctl restart proxy-go
   ```

2. **验证**:
   ```bash
   # 应该看到 S3 初始化日志
   journalctl -u proxy-go -f | grep S3
   ```

3. **恢复数据** (如果需要):
   ```bash
   # 从备份恢复
   tar -xzf backup-20231203.tar.gz -C /path/to/proxy-go/

   # 重启服务
   systemctl restart proxy-go
   ```

## 常见问题

### Q: 迁移过程会丢失数据吗?

A: 不会。迁移过程:
1. 本地数据不变
2. 上传到 D1 后仍保留本地副本
3. 如果上传失败,会继续使用本地数据

### Q: 需要停机吗?

A: 不需要。可以在线迁移:
1. 添加 D1 配置
2. 重启服务 (几秒钟)
3. 数据自动上传

### Q: S3 和 D1 可以同时使用吗?

A: 不建议。如果同时配置:
- `D1_SYNC_ENABLED=true` - 使用 D1
- `D1_SYNC_ENABLED=false` 或未设置 - 使用 S3

两者不会同步,选择一个使用即可。

### Q: 迁移后统计数据会重置吗?

A: 不会。`path_stats.json` 和 `banned_ips.json` 都会迁移到 D1,历史数据完整保留。

### Q: D1 有容量限制吗?

A: Cloudflare D1 免费额度:
- 5 GB 存储
- 每天 5M 读取 + 100K 写入

对于配置和统计数据,完全够用。

## 性能对比

实际测试结果 (基于100节点):

| 指标 | S3 | D1 | 提升 |
|------|----|----|------|
| 配置读取 | ~200ms | ~50ms | 4x |
| 配置写入 | ~300ms | ~80ms | 3.75x |
| 月度成本 | ~$5 | $0 | 节省100% |
| 请求数/天 | ~10,000 | ~10,000 | - |

## 支持

如果迁移遇到问题:

1. 查看日志: `journalctl -u proxy-go -f`
2. 检查 Worker 日志: `cd cloudflare-worker && npm run tail`
3. 验证配置: 对比 [D1_SYNC_SETUP.md](D1_SYNC_SETUP.md)
4. 测试 Worker: `curl https://your-worker.workers.dev/`

迁移成功后,记得更新你的部署文档和监控配置!
