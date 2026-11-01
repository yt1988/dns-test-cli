# dnstest

[English](README.md) · **简体中文**

哪家公共 DNS 在这台机器上最快？`dnstest` 测的是和 [dns.1ms.dog](https://dns.1ms.dog/zh-cn/) 同样的 33 家 DNS，但用的是浏览器发不出的协议，全部实测：**普通 DNS 的 UDP 和 TCP（53 端口）、DNS over TLS、DNS over HTTPS、DNS over QUIC**。单个二进制文件，没有依赖，适用于 VPS、Linux 主机、路由器（MIPS/ARM）和桌面系统。

```sh
curl -fsSL https://dns.1ms.dog/i | sh
```

这条命令把 `dnstest` 安装到 `/usr/local/bin`（非 root 时装到 `~/.local/bin`），然后打开一个简短的菜单：测全部 DNS 还是只测某些地区、某些协议，走 IPv4 还是 IPv6，测几轮，要不要把结果存成文件。选完会打印出等价的命令，下次不用菜单也能直接运行。Windows 在 PowerShell 里运行：

```powershell
irm https://dns.1ms.dog/i.ps1 | iex
```

其他用法：

```sh
curl -fsSL https://dns.1ms.dog/i | sh -s -- run          # 下载到临时目录跑一次，不安装
curl -fsSL https://dns.1ms.dog/i | sh -s -- --no-run     # 只安装
dnstest -p udp,dot -r cloudflare,google,alidns           # 只测部分协议、部分 DNS
dnstest -g cn,global -n 20                               # 只测中国大陆和全球的，20 轮
dnstest -o md                                            # 同时把结果存成 Markdown（或 -o json）
dnstest -6                                               # 走 IPv6
dnstest --json > result.json
```

安装脚本优先从 dns.1ms.dog 下载（它转发 GitHub 的发布文件，GitHub 慢或连不上时也能用），失败再从 GitHub 下载，并用发布里的 `checksums.txt` 校验 SHA-256。提供 Linux（amd64、arm64、armv7、386、riscv64、mips、mipsle）、macOS、FreeBSD、Windows 版本（[发布页](https://github.com/yt1988/dns-test-cli/releases)）。有 Go 的话也可以：`go install github.com/yt1988/dns-test-cli@latest`。

## 输出

```
   #  DNS                 UDP    TCP    DoT    DoH    DoQ    未缓存  DNSSEC  ECS
   1  AliDNS              4.1    4.3    4.8    5.2    5.6       253       –    –
   2  DNSPod              4.4    ×      5.9    6.1      ·       512       –    ✓
   …
› 最快：AliDNS（UDP），4.1 毫秒（未缓存 253 毫秒）
› 你的当前 DNS 192.168.1.1：18 毫秒；换成 AliDNS 每次解析约快 14 毫秒
  设置方法：把 DNS 服务器改成 223.5.5.5, 223.6.6.6
```

第二行显示测试从哪里发起（城市和网络，不显示 IP）。每格是 10 次热门域名查询的中位数，单位毫秒；`!` 表示有丢包，`×` 无应答，`·` 不支持。本机当前使用的 DNS（读取 `/etc/resolv.conf`、systemd-resolved 或 Windows 网卡设置）也一起测，换了能快多少一目了然。加 `-v` 每个 DNS、每种协议单独一行：地址、握手、中位数、P90、未缓存中位数、丢包，以及 DNS 的出口地址。

## 怎么测的

- **五种协议都用自己实现的客户端**，每次查询从发出到收齐应答计时。握手（TCP、TLS、QUIC）单独计时，不算进查询；服务器关闭了空闲连接时，在新连接上重发一次。
- **先预热，再交错测 10 轮。** 先把三个热门域名各查一次，放进每家 DNS 的缓存。之后每轮把每个「DNS × 协议」各查一次，顺序重新打乱，几个同时进行，网络偶尔卡一下影响的是所有 DNS。每隔一轮再查一个根本不存在的随机域名（未缓存，只显示不排名）。
- **只算真应答**：ID 和问题都要和查询一致。超时没有应答记为丢包。
- **拦截检测。** 开测前，先向 Cloudflare 网页服务的地址发普通 DNS 查询，那里根本不运行 DNS 服务。如果收到了回答，说明途中有人（代理、路由器或运营商）自己在回答 53 端口的查询：普通 DNS 那几行会标 `*`，不参与排名。部分域名被注入伪造应答的情况也用同样的方法检测。
- **DNSSEC**（`dnssec-failed.org` 必须失败，签名域名必须带 AD 标志）、**ECS**（是否把客户端子网传给权威服务器）、**NXDOMAIN 篡改**（不存在的域名却返回了地址）。
- 排名规则和网页一致：按缓存命中的中位数排，每 1% 丢包加 3%。

DoT、DoH、DoQ 的主机名先用系统解析器解析；如果失败，或者拿到的是代理的假地址（198.18.0.0/15），改为通过 1.1.1.1 和 223.5.5.5 的 DoH（按 IP 直连）解析。

## 选项

`dnstest -h` 列出全部选项：`-r` 指定 DNS，`-g` 指定地区（`global`、`cn`、`region`），`-p` 指定协议，`-4`/`-6`，`-n` 轮数，`-t` 超时，`-C` 并发，`-o md|json` 和 `--file` 存文件，`--json` 输出到标准输出，`-v`，`--lang`（10 种语言，菜单问一次后记住），`--offline`，`--resolvers FILE`，`--list`，`--no-system`，`--update`。

**更新。** dnstest 最多每小时向 dns.1ms.dog 查一次最新版本；在终端里会询问是否更新，用发布里的校验和核对新二进制，替换后重新运行。`dnstest --update` 立即更新；`--offline` 或 `DNSTEST_NO_UPDATE=1` 关闭。

## 隐私

每次运行会从 dns.1ms.dog 获取最新的 DNS 清单，并向 Cloudflare（`speed.cloudflare.com/meta`）查询测试是从哪里发起的，用于「出口」那一行：城市、国家和网络，IP 地址本身不显示也不保存。最多每小时向 dns.1ms.dog 查一次最新版本。`--offline` 三项都跳过。除了 DNS 查询本身，不向任何地方发送其他内容，结果也不会上传。

## 许可

MIT
