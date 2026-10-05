# SBoardNode

SBoardNode 是 SBoard 的极轻量 Linux 边缘 Agent。它只使用 Go 标准库，不包含 Web 服务、数据库或第三方框架。

## v1 功能

- 从 `/etc/sboardnode/config.json` 读取配置
- 定时向 `POST /api/node/v1/heartbeat` 上报心跳
- 上报主机 CPU、内存、运行时间以及 Xray 进程、PID 和监听端口
- 每 15 秒检查一次 Xray；停止时自动调用 OpenRC 或 systemd 重启服务
- 重启操作带 60 秒冷却，避免服务异常时形成重启风暴
- 支持 Alpine Linux/OpenRC 和 Debian/systemd
- 支持 `GOMEMLIMIT` 与 `GOGC`
- 首次安装默认同时安装 Xray，并生成一个 VLESS + Reality TCP 入站；已有 Xray 配置会被保留
- Agent 心跳会上报 Xray 入站的公开连接信息，不会上报 Reality 私钥

配置同步和 Xray API 流量统计保留到后续版本。

安装器可以通过 `--xray-port`、`--reality-sni`、`--reality-dest`、`--xray-uuid` 和 `--no-xray` 覆盖 Xray 行为。默认 Reality SNI 为 `www.cloudflare.com`，也可以通过 `SBOARD_REALITY_SNI` 环境变量指定。已有 `/etc/xray/config.json` 时安装器不会覆盖配置，只会校验并启动 Xray 服务。

## 配置

```json
{
  "server": "https://sboard.example.com",
  "node_id": "SBoard 生成的节点 ID",
  "token": "SBoard 生成的 Agent Token",
  "heartbeat_interval": 60
}
```

`server_url` 也可以作为 `server` 的兼容字段使用。`heartbeat_interval` 可设置为 10–3600 秒，省略时为 60 秒。

## 一键安装

GitHub Release 发布后，可以直接运行：

```sh
curl -fsSL https://raw.githubusercontent.com/TheFunny233/SBoardNode/main/install.sh | sudo sh
```

脚本会在终端中提示输入 SBoard 地址、Node ID 和 Agent Token。也可以显式传参：

```sh
curl -fsSL https://raw.githubusercontent.com/TheFunny233/SBoardNode/main/install.sh | \
  sudo sh -s -- \
    --server https://sboard.example.com \
    --node-id YOUR_NODE_ID \
    --token YOUR_AGENT_TOKEN
```

Token 作为参数时可能被 shell 历史记录保存，交互式输入更安全。脚本会：

- 根据 `amd64`、`arm64` 或 `armv7` 下载静态二进制
- 使用 Release 中的 `sha256sums.txt` 验证文件
- 写入权限为 `0600` 的配置和运行环境文件
- 自动创建并启动 OpenRC 或 systemd 服务
- 升级时默认保留已有配置

指定版本：

```sh
sudo sh install.sh --version v0.2.0
```

## 内存调优

安装脚本为 Agent 创建 `/etc/sboardnode/environment`，默认根据物理内存和当前 cgroup 限制自动选择：

```text
GOMEMLIMIT=20MiB
GOGC=50
```

Agent 策略保持保守：有效内存不超过 256MiB 时使用 `20MiB`，不超过 512MiB 时使用 `24MiB`，更大的机器使用 `32MiB`。

安装器还会创建独立的 `/etc/sboardnode/core-environment` 并通过 systemd drop-in 或 OpenRC `/etc/conf.d/xray` 传给 Xray：

| 有效内存 | Xray `GOMEMLIMIT` |
| --- | --- |
| ≤128MiB | 内存的 5/16，最小 32MiB；128MiB 时为 40MiB |
| 129–1024MiB | 内存的 1/2 |
| >1024MiB | 最大 512MiB |

有效内存取 `/proc/meminfo`、当前进程 cgroup v2 `memory.max`/`memory.high` 和 cgroup v1 `memory.limit_in_bytes` 中最小的有效值。Xray 正在运行时，安装器会重启一次服务以应用设置。

可以在安装时覆盖：

```sh
sudo sh install.sh --gomemlimit 24MiB --gogc 75
```

两个值都直接传给 Go Runtime，因此也可以按需使用 `--gomemlimit off` 或 `--gogc off`。默认值更适合 128MB 小内存主机。

覆盖 Xray 设置或让安装器停止管理 Xray 内存：

```sh
sudo sh install.sh --core-gomemlimit 64MiB --core-gogc 75
sudo sh install.sh --no-core-memory-tuning
```

`GOMEMLIMIT` 是 Go 堆内存软限制，不是 RSS 硬上限；TLS、线程栈、socket 缓冲和 mmap 仍可能令 RSS 高于该值。

修改环境文件后重启服务：

```sh
# Alpine Linux
rc-service sboardnode restart

# Debian
systemctl restart sboardnode
```

## 手工构建

需要 Go 1.24 或更高版本：

```sh
make test
make vet
make build
```

输出文件为 `bin/sboardnode`，构建时固定 `CGO_ENABLED=0`，可直接复制到 Alpine Linux。

单次诊断心跳：

```sh
sboardnode -config /etc/sboardnode/config.json -once
```

## 服务日志

```sh
# Alpine Linux
rc-service sboardnode status

# Debian
journalctl -u sboardnode -f
```

## 资源验收目标

最终发布前应在限制为 128MB 的 Alpine Linux 环境中同时运行 SBoardNode 与 Xray，分别记录以下场景的 RSS 峰值：

1. 空闲稳定运行
2. 正常心跳
3. Xray 异常检测和自动重启
4. 配置同步（v2 实现后）

SBoardNode 的目标为空闲 RSS 小于 30MB、峰值小于 50MB。

当前 v1 已完成 Docker 中的 Alpine 3.22、128MB 内存限制验收：

- SBoardNode 完成真实心跳时，容器总内存约 4.0MiB。
- SBoardNode 与轻量 Xray 测试进程共同运行并完成异常重启时，容器总内存峰值约 9.9MiB。
- 服务端确认收到的内存总量为 128MiB，Xray 进程状态和监听端口均可正常上报。

第二项使用的是用于验证进程、端口和 OpenRC 重启流程的轻量测试程序，不代表真实 Xray-core 的内存占用；发布前仍需使用目标 Xray 配置在纯 Alpine 主机上补充最终联合 RSS 测量。
