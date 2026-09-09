# ZeroDeNet 共享插件市场设计

共享设计已迁入 [zerodenet/plugins 的市场草案](https://github.com/zerodenet/plugins/blob/main/docs/marketplace-design.md)，后续统一在共享仓库维护。

OAuth 插件源码独立维护于 [higanbana986/zboard-oauth](https://github.com/higanbana986/zboard-oauth)。共享市场主分支分别维护 `catalogs/zboard.json` 与 `catalogs/znet-sink.json`，收录各插件仓库发布的发行元数据。开发、使用和发布规范见各仓库 README。

共享仓库与独立插件仓库已建立，市场自动化通过审核 PR 同步发行元数据；正式签名安装目录与客户端插件运行时尚未发布。当前 ZBoard 继续使用本仓库描述的 v1 目录及宿主生命周期实现，不因源码迁移改变安装协议。
