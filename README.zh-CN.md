# ZBoard

[English](README.md) | [简体中文](README.zh-CN.md)

**面向自用的 Zero 代理服务管理面板。**

ZBoard 以最小核心满足日常管理需要，通过插件按需增强能力。节点部署、服务配置和订阅管理可以在同一个 Web 控制台中完成。

管理员在后台配置服务、分配访问权限；用户在独立的账户中心管理订阅、获取客户端配置、查看用量和提交工单。[Zero Core](https://github.com/zerodenet/core) 运行在节点上，负责处理代理流量。

[开始使用](https://docs.zerodenet.org/projects/zboard/guides/installation) · [下载版本](https://github.com/zerodenet/zboard/releases) · [项目文档](https://docs.zerodenet.org/projects/zboard/guides/) · [反馈问题](https://github.com/zerodenet/zboard/issues)

## 可以用它做什么

| 功能 | 说明 |
| --- | --- |
| 节点管理 | 管理服务器与 SSH 连接，安装 Zero、发布配置并查看部署结果。 |
| 协议服务 | 配置 VLESS、VMess、Shadowsocks、Trojan、Hysteria2 和 Mieru，通过节点组组织访问权限；协议是否可用取决于节点安装的 Zero 内核。 |
| 网络前置 | 通过转发节点访问落地服务，同一节点上的多个转发入口可以共用代理池。 |
| 订阅配置 | 生成 Zero、Clash/Mihomo 和 sing-box 配置，管理订阅模板、路由规则、策略组和节点过滤。 |
| 用户与订单 | 管理套餐、计费选项、订单、续费、订阅有效期和流量额度，由管理员确认订单后开通服务。 |
| 用量与支持 | 查看订阅流量、任务执行结果和审计记录，发布公告并处理用户工单。 |

## 开始使用

Docker 镜像已包含后端和 Web 控制台。[Releases](https://github.com/zerodenet/zboard/releases) 同时提供 Linux amd64 二进制包和 Docker 镜像离线包。部署时请选择明确的 Release 标签，并查看对应的发布说明。

使用 Docker 部署时，需要准备 Docker Compose、数据库，以及配置 HTTPS 的域名。按照[首次安装指南](https://docs.zerodenet.org/projects/zboard/guides/installation)启动服务后，访问 `/setup` 创建第一个管理员。

安装完成后，可以按以下顺序配置第一条服务：

1. 添加节点并安装 Zero。
2. 创建协议服务，等待节点配置发布成功。
3. 将服务加入节点组，再将节点组关联到套餐。
4. 通过订单流程为用户开通订阅。
5. 在兼容客户端中导入用户的订阅，连接后在控制台查看流量用量。

[ZNet Sink](https://github.com/zerodenet/znet-sink) 是 Zero 生态的桌面客户端，也可以选择兼容的 Clash/Mihomo 或 sing-box 客户端，并使用对应的订阅格式。

## 插件系统

插件系统用于在最小核心之上扩展功能。你可以按需安装现有插件，也可以开发插件满足特定需求。

插件支持从市场安装或通过 `.zbplugin` 安装包导入，并在后台统一管理配置、启停和更新。

[插件使用指南](https://docs.zerodenet.org/projects/zboard/plugins/) · [插件市场与安装](https://docs.zerodenet.org/projects/zboard/plugins/marketplace) · [插件开发](https://docs.zerodenet.org/projects/zboard/plugins/development)

## 使用文档

- [首次安装](https://docs.zerodenet.org/projects/zboard/guides/installation)
- [Docker 存储与备份](deploy/docker/README.md)
- [节点安装与维护](https://docs.zerodenet.org/projects/zboard/guides/node-management)
- [网络前置与共享代理池](https://docs.zerodenet.org/projects/zboard/guides/network-fronting)
- [订阅节点过滤](https://docs.zerodenet.org/projects/zboard/guides/subscription-filtering)
- [本地开发](https://docs.zerodenet.org/projects/zboard/contributing/development)与[参与贡献](CONTRIBUTING.md)
- [API 参考](backend/api/openapi.yaml)

其他专题可以从[文档导航](https://docs.zerodenet.org/projects/zboard/guides/)查找。

## 技术基础

后端使用 Go、go-zero 和 GORM，前端使用 Vue 3、Vite、Pinia 和 PrimeVue；提供 MySQL 8 与 SQLite 数据库驱动，节点运行时为 Zero。

## 开源协议

ZBoard 使用 [Mozilla Public License 2.0](LICENSE) 授权。
