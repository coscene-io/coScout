# Master-Slave 架构使用指南

## 概述

cos-agent 现在支持 master-slave 架构，允许一台联网的主机（master）管理多台内网机器（slave），实现分布式数据采集和统一上传。

## 架构特点

### 核心功能
1. **Slave 自动注册**: slave 节点启动时自动向 master 注册
2. **分布式任务分发**: master 将任务分发给所有在线的 slave 节点
3. **文件聚合**: master 收集所有 slave 的文件信息并统一上传
4. **Rule模式支持**: rule 引擎触发的 collectInfo 也支持 slave 文件扫描
5. **心跳保活**: slave 定期向 master 发送心跳保持连接
6. **自动故障处理**: 超时的 slave 会被自动清理，请求失败会被忽略

### 设计原则
- **零破坏性**: 现有单机模式完全兼容，不受影响
- **统一架构**: task、rule、upload 模块全部支持 master-slave，代码逻辑统一
- **简单可靠**: 使用 HTTP API 通信，无复杂依赖
- **容错性强**: 单个 slave 故障不影响整体功能
- **优雅降级**: 未启用 master-slave 时自动使用单机模式
- **资源高效**: 文件按需下载，上传后自动清理缓存

## 配置方式

### 方式一：配置文件 (推荐)

在 `config.yaml` 中添加以下配置：

```yaml
# Master-Slave架构配置（简化版）
master_slave:
  # 是否启用master模式（默认false）
  enabled: true
```

### 默认配置说明

所有配置都使用优化的默认值：
- **通信端口**: 22525 (master和slave都使用此端口)
- **超时时间**: 5秒 (包括请求超时和slave超时)
- **心跳间隔**: 3秒
- **文件传输大小**: 10MB
- **最大slave数量**: 无限制
- **缓存目录**: `/tmp/cos-master-cache`
- **临时目录**: `/tmp/cos-slave`

### 方式二：命令行参数

#### 启动 Master 节点
```bash
# 方式1: 使用配置文件启动master模式
./cos daemon --config-path=config.yaml

# 配置文件中设置 master_slave.enabled: true
```

#### 启动 Slave 节点
```bash
# 基本启动
./cos slave --master-addr=192.168.1.100:22525

# 带文件夹前缀启动（推荐）
./cos slave --master-addr=192.168.1.100:22525 --file-prefix="device1"

# 完整参数启动
./cos slave --master-addr=192.168.1.100:22525 --port=22525 --slave-id=slave-001 --file-prefix="device1"
```

### 文件夹前缀功能

使用 `--file-prefix` 参数可以为slave的文件自动添加文件夹前缀，方便区分不同设备的文件：

```bash
# 设备1
./cos slave --master-addr=192.168.1.100:22525 --file-prefix="camera1"

# 设备2  
./cos slave --master-addr=192.168.1.100:22525 --file-prefix="sensor2"

# 设备3
./cos slave --master-addr=192.168.1.100:22525 --file-prefix="robot3"
```

上传的文件将变成：
- `camera1/data.log` 
- `sensor2/output.csv`
- `robot3/debug.txt`

### 文件组织效果

使用文件夹前缀后，上传到云端的文件将按设备分类组织：

```
云端文件结构：
├── camera1/
│   ├── video_001.mp4
│   ├── image_capture.jpg
│   └── camera_log.txt
├── sensor2/
│   ├── temperature_data.csv
│   ├── humidity_data.csv
│   └── sensor_status.log
└── robot3/
    ├── motion_plan.json
    ├── navigation_log.txt
    └── debug_info.log
```

这样可以：
- 清晰区分不同设备的文件
- 便于文件管理和查找
- 避免文件名冲突
- 支持设备级别的权限控制

## 部署示例

### 典型网络拓扑
```
Internet ←→ Master (192.168.1.100:22525) ←→ Slave1 (192.168.1.101:22525)
                         ↑                       ↑
                         ↓                       ↓
                    Slave2 (192.168.1.102:22525)  Slave3 (192.168.1.103:22525)
```

### 部署步骤

#### 1. 部署 Master 节点
```bash
# 在有外网连接的机器上
cd /path/to/cos-agent

# 修改配置文件
vim config.yaml
# 设置 master_slave.enabled: true

# 启动master
./cos daemon --config-path=config.yaml
```

#### 2. 部署 Slave 节点
```bash
# 在每台内网机器上
cd /path/to/cos-agent

# 启动slave，带上设备文件夹前缀
./cos slave --master-addr=192.168.1.100:22525 --file-prefix="device1"
```

#### 3. 验证部署
```bash
# 查看master日志，应该看到slave注册信息
tail -f cos.log | grep "Slave.*registered"

# 查看slave日志，应该看到注册成功和心跳信息  
tail -f cos.log | grep "Successfully registered\|Heartbeat sent"
```

## 工作流程

### 1. 初始化阶段
1. Master 启动 HTTP 服务器监听 slave 连接
2. Slave 启动后自动向 master 注册
3. Slave 开始定期发送心跳保持连接

### 2. 任务处理阶段

**统一增强架构**：
- Task Handler、Rule Handler、Upload Collector 均自动支持 master-slave
- 启用时：同时处理本地和 slave 文件
- 未启用时：只处理本地文件，与单机模式相同

**Task模式（云端任务）**：
1. Master 接收到云端任务
2. Master 并发请求所有在线 slave 扫描文件
3. Master 聚合本地文件和所有 slave 文件信息
4. Master 创建统一的上传任务

**Rule模式（本地规则）**：
1. Rule 引擎触发，创建 collectInfo
2. Master 定时扫描 collectInfo 文件
3. Master 同时扫描本地文件和所有 slave 文件
4. Master 合并文件信息并创建上传任务

### 3. 文件上传阶段
1. Master 开始上传流程
2. 遇到 slave 文件时，从对应 slave 下载到本地缓存
3. 从缓存上传文件到云端
4. 上传完成后清理缓存

### 4. 文件路径标准化
使用标准的 `slave://` 协议前缀标识 slave 文件：
- **格式**: `slave://slaveID/absolutePath`
- **Slave ID**: 固定16字符长度，仅包含字母数字
- **路径**: 绝对路径，以 `/` 开头
- **示例**:
  - `slave://f47ac10b58cc4372/var/log/system.log`
  - `slave://6ba7b8119dad11e1/home/robot/data.csv`
  - `slave://b0018a1e9ce44c2f/etc/config.yaml`

## API 接口

### Master 端点
- `POST /api/v1/slave/register` - slave 注册
- `POST /api/v1/slave/heartbeat` - slave 心跳
- `POST /api/v1/slave/unregister` - slave 注销
- `GET /api/v1/slaves` - 获取 slave 列表

### Slave 端点  
- `POST /api/v1/files/scan` - 文件扫描请求
- `POST /api/v1/files/download` - 文件下载请求
- `GET /api/v1/health` - 健康检查

## 故障处理

### 常见问题

#### 1. Slave 无法连接 Master
**症状**: slave 日志显示连接被拒绝
```
ERROR Failed to register with master: connection refused
```

**解决方案**:
- 检查 master 是否启动: `ps aux | grep cos`
- 检查防火墙设置: `sudo ufw status`
- 检查网络连通性: `telnet master_ip 8080`

#### 2. Slave 注册失败
**症状**: slave 日志显示注册失败
```
ERROR Registration failed: Maximum number of slaves reached
```

**解决方案**:
- 增加 master 配置中的 `max_slaves` 值
- 检查是否有过期的 slave 占用名额

#### 3. 文件传输失败
**症状**: master 日志显示下载失败
```
ERROR Failed to download from slave: timeout
```

**解决方案**:
- 检查 slave 服务状态
- 增加 `request_timeout` 配置
- 检查文件权限和磁盘空间

### 监控和调试

#### 查看 Master 状态
```bash
# 查看已注册的slave
curl http://localhost:22525/api/v1/slaves

# 查看master日志
tail -f cos.log | grep "Master\|Slave"
```

#### 查看 Slave 状态
```bash
# 健康检查
curl http://slave_ip:22525/api/v1/health

# 查看slave日志
tail -f cos.log | grep "Heartbeat\|Register"
```

## 最佳实践

### 网络配置
1. 确保 master 和所有 slave 在同一网络段
2. 配置静态 IP 避免地址变化
3. 开放必要的端口（默认 22525）

### 性能优化
1. 文件传输大小已优化为 10MB，适合大多数场景
2. 超时时间已优化为 5秒，快速失败避免阻塞
3. 心跳间隔 3秒，快速检测网络异常

### 安全考虑
1. 在生产环境中应该添加认证机制
2. 考虑使用 HTTPS 加密通信
3. 限制网络访问，仅允许必要的 IP 连接

### 扩展性
1. 单个 master 建议不超过 50 个 slave
2. 大规模部署可以考虑多 master 架构
3. 文件较大时考虑实现断点续传

## 升级兼容性

从单机模式升级到 master-slave 模式：

1. **无缝升级**: 现有配置保持不变，只需设置 `master_slave.enabled: false`（默认值）
2. **渐进迁移**: 可以先在一台机器上启用 master 模式，逐步添加 slave
3. **回退方案**: 任何时候都可以通过设置 `enabled: false` 回退到单机模式

## 技术细节

### 文件标识规则
- 本地文件: 使用绝对路径标识
- Slave 文件: 使用 `{slave_id}:{absolute_path}` 格式标识

### 超时和重试机制
- Slave 注册: 失败后 5 秒重试
- 心跳: 失败 3 次后标记为离线
- 文件请求: 单次超时后跳过，不影响其他操作

### 缓存管理
- 文件按需下载到 master 缓存目录
- 上传完成后自动清理缓存
- 支持手动清理: 重启 master 服务

Master-slave 架构让 cos-agent 能够灵活应对复杂的网络环境，在保持简单易用的同时提供强大的分布式数据采集能力。
