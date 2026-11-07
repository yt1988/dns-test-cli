# AGENTS.md

给在这个仓库里干活的人和 AI 代理（Claude Code、Codex、Cursor 等都适用）。

## 这是什么

dnstest：dns.1ms.dog 的命令行版。在服务器、路由器或电脑上，用 UDP、TCP、DoT、DoH、DoQ 五种协议实测公共 DNS，并和这台机器当前用的 DNS 对比。纯 Go，单个静态二进制，支持 13 个平台。网页版和 DNS 列表在 [yt1988/dns-test](https://github.com/yt1988/dns-test)。

先读 [README.md](README.md)（显示什么、怎么测、参数、隐私），再看 [docs/design.md](docs/design.md)（设计取舍）。1ms.dog 全站的运营说明在私有仓库 1ms-shell，不在这里。

## 文件

| 文件 | 做什么 |
|---|---|
| `main.go` | 参数、帮助文字、入口 |
| `menu.go` | 不带参数运行时的交互菜单 |
| `measure.go` | 测量：预热、轮次、超时、无应答率、排名（和网页一致） |
| `transport.go`、`wire.go` | 五种协议的查询，DNS 报文 |
| `output.go`、`progress.go` | 终端表格、`-v`、JSON 和 Markdown 输出 |
| `texts.go`、`i18n.go`、`lang_*.go` | 10 种语言的文字，以及按系统语言选择 |
| `region.go` | 「从哪里测」那一行的地名：所有国家和地区的名称都经过这里 |
| `resolvers.go`、`resolvers.json` | DNS 列表：运行时先取 dns.1ms.dog/resolvers.json，取不到才用内置的这份 |
| `sysdns_*.go` | 读这台机器当前用的 DNS |
| `update.go`、`origin.go` | 检查新版本、自动更新（核对 checksums） |
| `install.sh`、`install.ps1` | 安装脚本，随版本一起发布；dns.1ms.dog/i 会转发它们 |

## 本地跑和测试

需要 Go（版本见 `go.mod`）：

```bash
go build -o dnstest . && ./dnstest -n 4 -v
gofmt -l . ; go vet ./... ; go test ./...
```

本机没有 Go 也可以：开一个 PR，GitHub Actions 的 `ci` 工作流会跑 gofmt、vet、test、编译，再从 GitHub 的网络里实际跑两次（`-n 4 -v`，以及只测三家、输出 JSON）。CI 通过后再合并。按改动范围选：

| 改了什么 | 要看什么 |
|---|---|
| 文字 `texts.go` | `go test`（`texts_test.go` 会检查 10 种语言的键和格式占位符是否一致），再实际看一眼输出 |
| 测量、协议 | CI 的实际运行，以及 `-v` 每行的数字是否合理 |
| 安装脚本 | `sh -n install.sh`（CI 也会跑），在一台干净的机器上装一次 |

## 发版

- 推送 `vX.Y.Z` 这样的 tag，`release` 工作流就会编译 13 个平台，写 `checksums.txt` 和来源证明，然后发布 GitHub Release。dns.1ms.dog 的 `/v`、`/dl/`、`/i` 会转发最新版，给连不上 GitHub 的地方用。
- 合并到 `main` 不等于发版，打 tag 才发。
- 发完核对三处：Release 上的文件齐不齐，`https://dns.1ms.dog/v` 是否返回新版本号，`https://dns.1ms.dog/dl/vX.Y.Z/checksums.txt` 是否返回 200。
- 只改了 DNS 列表的话，不用发版：工具运行时会先取网站上的列表，`resolvers` 工作流每天也会把内置的那份同步一次。

## 规矩

- 10 种语言都要有。数量放在名词后面写（「resolvers: 3 · rounds: 1」），避开各语言的复数变化。
- 术语：超时没收到应答叫「无应答率 / no-answer rate」，不叫丢包。DNS 服务器限速或丢弃查询也会没应答，这不是网络丢包。JSON 里的字段名 `loss` 不改。
- 地名一律经过 `region.go`，不要在别处另写。
- 隐私：除了 DNS 查询本身，只取 dns.1ms.dog 的列表和版本号，再问一次 Cloudflare 本机在哪里（只用城市和网络，不显示、不保存 IP）。不要加别的上传。
- 提交说明用英文，文档用中文。
