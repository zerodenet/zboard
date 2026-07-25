# Zboard 管理端前端系统重构实施计划

状态：产品取舍已确认；规模化样板、历史游标、全表格共享承载层、领域输入、请求前共享校验、版本化字段错误、视觉令牌单一源、外壳样式所有权、路由级按需交付及任务恢复集成已落地，正在准备完整交互验收。

更新时间：2026-07-22

## 1. 实施原则

- 以统一系统替换页面私有实现，不在旧结构上继续增加局部 CSS 补丁。
- 每一阶段同时交付公共能力、真实页面、接口契约和验证证据，禁止只完成组件库或静态样式。
- 迁移期间保留现有业务行为与 `docs/data-model.md` 资源边界；界面组合不能改变数据所有权。
- 新旧 API 如需并存，兼容期必须有明确删除条件，不长期保留两个响应模型。
- 当前工作树已有大量未提交改动。实施前先重新检查状态和重叠文件，不覆盖或回滚用户已有工作。
- 未经明确授权不执行 Git staging、commit、push 或 release。
- 完整目标完成前不把某个样板页面通过视为整体完成。

## 2. 目标目录结构

现有公共组件迁移为按职责组织的结构：

```text
frontend/src/
  api/
    client.ts
    contracts.ts
    errors.ts
    pagination.ts
  components/
    primitives/
      UiButton.vue
      UiInput.vue
      UiTextarea.vue
      UiSelect.vue
      UiCheckbox.vue
      UiSwitch.vue
      UiIcon.vue
      StatusBadge.vue
    forms/
      FormField.vue
      NumberField.vue
      MoneyField.vue
      BytesField.vue
      MultiplierField.vue
      FormAlert.vue
    data/
      DataWorkbench.vue
      DataTable.vue
      FilterBar.vue
      PaginationBar.vue
      BulkActionBar.vue
      ColumnPicker.vue
      LookupSelect.vue
    feedback/
      FeedbackHost.vue
      PageAlert.vue
      SectionAlert.vue
      ConfirmDialog.vue
      ResultDialog.vue
      TaskTray.vue
    overlays/
      ModalDialog.vue
      DetailDrawer.vue
    layout/
      PageHeader.vue
      UiSection.vue
      UiTabs.vue
      EmptyState.vue
      LoadingState.vue
  composables/
    useUrlQueryState.ts
    useRemoteTable.ts
    useAbortableRequest.ts
    useMutationState.ts
    useDirtyForm.ts
  domain/
    formatters.ts
    statuses.ts
    adapters.ts
  styles/
    tokens.css
    reset.css
    admin-shell.css
    components.css
    utilities.css
```

迁移完成后删除根级旧包装组件、重复 class 推断和失去路由的 `Billing.vue`。迁移过程中可以保留薄转发组件，但必须记录删除阶段。

## 3. 阶段 A：基础契约

### 3.1 设计令牌

把颜色、字体、间距、圆角、控件高度、表格密度、层级和动画拆入令牌：

- 控件默认高度 40px，紧凑高度 32px；
- 表格紧凑行 40px、舒适行 48px；
- 字段 label、hint、error 各自拥有固定语义与间距，不通过页面选择字号；
- surface、border、text、muted、primary、success、warning、danger、info 只从令牌读取；
- z-index 明确 sidebar、topbar、drawer、dialog、confirm、toast 的顺序；
- `prefers-reduced-motion` 关闭非必要过渡。

验收证据：令牌快照、全局搜索不存在页面硬编码重复控件高度、四个目标宽度的浏览器截图和 DOM 尺寸检查。

### 3.2 Primitive 组件

`UiInput`：

- 显式接收并转发 `type`、`value`、`min/max/step`、autocomplete、inputmode 和 ARIA 属性；
- checkbox/switch 从文本输入中拆分，不通过 `type` 在同一组件切换完全不同控件；
- `v-model.trim` 只在提交/blur 或 adapter 中规范化，避免每次输入截断用户空格；
- readonly、disabled、invalid 和 loading 状态一致。

`UiSelect`：

- 使用结构化 options，不解析 `<option>` VNode；
- 支持 option group、disabled、placeholder、clear 和可访问名称；
- 远程对象选择由 `LookupSelect` 负责，不把 1000/5000 条选项塞入普通 Select。

`UiButton`：

- 使用 `variant="primary|secondary|ghost|danger"` 和 `size="sm|md"`；
- 不再解析 `.button-*` class；
- loading 状态保留按钮宽度、禁用重复提交并提供可读文本。

验收证据：组件交互测试覆盖属性转发、键盘、invalid、disabled、loading、readonly 和复选语义。

### 3.3 FormField

建议 API：

```vue
<FormField
  id="ssh-port"
  label="SSH 端口"
  required
  description="节点管理通道端口"
  hint="范围 1–65535"
  :error="errors.ssh_port"
>
  <UiInput v-model.number="form.ssh_port" type="number" min="1" max="65535" />
</FormField>
```

FormField 负责生成 `for/id`、required/optional、`aria-describedby`、`aria-invalid`、hint/error 槽位和稳定的网格对齐。页面不得再用复杂 `<label>` 包裹按钮、卡片或多层布局。

### 3.4 反馈

替换 `TransientFeedback`：

- `PageAlert` 持久显示页面加载失败和全局数据缺口；
- `SectionAlert` 绑定局部列表或详情加载失败；
- `FormAlert` 显示无法映射到字段的提交错误；
- Toast 只用于复制、短暂保存成功等非阻塞结果；
- Confirm 支持队列或显式单实例拒绝，不能静默取消已有确认；
- TaskTray 显示发布、检测和批量任务直到终态。

错误 adapter 输出：稳定错误码、用户文案、字段 map、是否可重试、技术详情和 trace/request ID。

## 4. 阶段 B：数据工作台

### 4.1 类型

```ts
type SortSpec = { field: string; direction: 'asc' | 'desc' }

type PageMeta = {
  offset: number
  limit: number
  total: number
  next_cursor?: string | null
  previous_cursor?: string | null
}

type ListResponse<T, A = Record<string, number>, F = Record<string, unknown>> = {
  items: T[]
  page: PageMeta
  aggregates: A
  facets: F
}
```

client 不再返回 `any[]`。所有管理列表拥有 summary、detail、filter 和 aggregate 类型。

### 4.2 `useRemoteTable` / `useCursorTable`

职责：

- 从 route query 解析并校验 q、filter、sort、offset/limit 或 cursor；
- 修改筛选时使用 router replace，明确提交的导航使用 push；
- 取消上一请求或使用 generation 防止过期响应覆盖；
- 区分 initialLoading、refreshing、pageError 和 staleData；
- 暴露 refresh、retry、setPage、setSort、setFilters；
- 记录选中范围是当前页还是全部筛选结果。
- 追加型历史页由 `useCursorTable` 消费服务端 `next_cursor`/`previous_cursor`，不在客户端推导深页 offset。

### 4.3 `DataWorkbench`

插槽/属性：columns、rows、loading、error、pagination、selection、filters、bulk actions、row menu、empty state、detail drawer。

统一行为：

- sticky header、固定选择列和操作列；
- 默认 50 条，允许 25/50/100；
- 行点击打开详情，行内交互阻止误触；
- 表格 caption、排序状态和选择范围可被屏幕阅读器理解；
- 移动端保留核心列，其余进入行摘要/详情，不把所有列压缩成不可读文本；
- 数据规模超过一页时绝不在浏览器下载全部结果后本地筛选。

## 5. 阶段 C：后端列表契约

### 5.1 公共分页

在 handler 公共层统一：

- 参数解析和允许排序字段白名单；
- `offset/limit` 默认和上限；
- cursor 编解码用于运行/审计类追加流；
- `{items,page,aggregates,facets}` 响应；
- lookup 响应只返回 id、label、secondary、status。

OpenAPI 为每个 filter、sort 和响应 schema 提供声明，测试保证文档路径和 handler 一致。

### 5.2 Nodes

列表 DTO 只包含资产摘要和批量聚合状态，不返回 SSH 密文、凭证或大配置。

查询通过一次 Nodes 主查询加批量聚合完成：

- KernelState 使用 JOIN/Preload 的固定查询；
- 协议数量按 node_id GROUP BY；
- 运行异常和凭证状态通过明确字段/聚合，不逐节点查询；
- q、region、lifecycle、enabled、connector、ssh、kernel、has_protocol filters；
- 排序白名单包含 name、region、last_seen、kernel、protocol_count、created_at。

### 5.3 ProtocolEndpoints

删除每端点 5 次 `loadProtocolEndpointUsage` 调用：

- FlowUsage 在线数按 endpoint GROUP BY；
- ProtocolCredential 有效数和 MAX(last_used_at) 按 endpoint GROUP BY；
- TrafficRecord 累计/今日按 endpoint 条件聚合；
- 最新 ProtocolDeployment 使用 group max/join 或窗口查询一次关联；
- 聚合范围只覆盖当前页 endpoint IDs，避免每次统计整个历史库；
- q、node、region、protocol、active、deployment、used filters。

列表响应不包含解密后的 server config。完整配置只由 detail handler 在授权后返回。

### 5.4 其余列表

| 资源 | 必要改造 |
| --- | --- |
| Users | total、q、status、is_admin、created 排序；用户业务统计批量聚合 |
| Subscriptions | 分页、user lookup、plan、status、expiry、quota filters；过期处理与只读查询解耦 |
| Orders | 分页、trade/user/status/time filters；Plan/SKU 快照摘要 |
| Plans | 管理列表 summary 与 SKU detail 分离；消除逐 Plan 查询 NodeGroup |
| NodeGroups | 分页；成员数、套餐数聚合；成员 picker 使用 endpoint lookup；筛选全集由有上限、带解析时间的 ID-only 服务端快照表达，加入/移除不逐页下载详情；字段和完整成员使用同一 revision，冲突必须重载 |
| TrafficRecords | 时间范围和分页为必填默认；避免无界全表返回 |
| Reconciliation | 服务端分页异常队列；正常汇总只返回 aggregates |
| Tasks | 返回 total/page；TaskItems 独立分页；运行状态可刷新/订阅 |
| Templates | 分页 summary；模板正文只在 detail 返回 |
| Tickets | 统一新响应壳和 URL 状态；保留现有最近活动排序 |
| OperationLogs | cursor 优先；结构化 error/output detail 按需读取 |
| AuditLogs | 时间范围、cursor/offset 和 detail；敏感字段过滤测试 |

## 6. 阶段 D：样板页面

### 6.1 Nodes 文件级工作

- `backend/internal/handler/handlers.go`：新增分页摘要 handler，保留 detail/update 资源边界；
- `backend/internal/server/router.go`：如采用新 admin path，注册兼容和 detail 路由；
- `backend/api/openapi.yaml`：filters、sort、summary/detail schemas；
- `frontend/src/api/contracts.ts`：NodeSummary、NodeDetail、NodeFilters；
- `frontend/src/views/Nodes.vue`：只组合 DataWorkbench、DetailDrawer 和领域面板；
- 内核、协议、凭证三个标签按需加载，使用 request generation；
- 批量检测/维护进入 Task API，不在浏览器串行调用节点接口。

节点样板验收：

- 1000 节点 fixture 首屏只返回 50 条；
- 筛选、排序、分页和 `node_id/tab` 可刷新恢复；
- 快速点击 20 个节点不出现详情串台；
- 未打开协议标签时不请求 endpoint API；
- 密钥和 SSH 私密字段不会出现在列表响应和 DOM；
- 390px 宽度下核心操作可达。

### 6.2 Protocols 文件级工作

- `backend/internal/handler/handlers.go`：批量聚合 summary 查询；
- `backend/internal/handler/operation_logs.go` 或独立文件：最新部署和批量发布任务；
- `frontend/src/views/Protocols.vue`：卡片列表替换为 DataWorkbench；
- 创建向导保留，但查看详情、配置编辑和发布历史分离；
- 行内倍率使用 MultiplierField，错误固定在该行；
- 批量发布返回 task ID 并进入 TaskTray。

协议样板验收：

- 5000 endpoint fixture 首屏只返回 50 条；
- 一页 list SQL 数量为常数；
- 任意 endpoint 最新发布状态准确，不依赖最近 200 条全局记录；
- 发布失败行展示摘要，详情可见完整结构化错误和日志入口；
- 按节点分组与普通表格共享同一 server query，不重新下载全部端点。

## 7. 阶段 E：全页面迁移批次

### 批次 1：基础设施闭环

Nodes、Protocols、NodeGroups、Traffic、Tasks、OperationLogs、AuditLogs。

目的：先验证大数据、批量任务、运行反馈和日志详情共同使用一套系统。

### 批次 2：客户与商业闭环

Users、Subscriptions、Tickets、Orders、Plans、SubscriptionTemplates。

目的：验证对象关联、交易确认、权益快照、模板编辑和支持时间线。

### 批次 3：入口与配置

Dashboard、Settings、AdminLayout。

目的：让 Dashboard 只消费各域 aggregates/queues，让 Settings 验证 schema form、dirty state 和 revision 冲突。

每一批完成后执行一次跨页面回归，但不在全部页面迁移前删除仍被旧页面使用的兼容组件。

## 8. 自动化测试计划

### 8.1 Backend

- handler filter/sort/pagination/table-driven tests；
- response schema 与 OpenAPI path 测试；
- 1000/5000 fixture 下返回条数、total 和聚合正确性；
- SQL 查询次数测试或带计数 logger 的上限断言，证明不随行数增长；
- 批量任务幂等、部分失败、重试和权限测试；
- 列表响应敏感字段缺失测试；
- UTF-8 错误、日志和模板输出测试。

### 8.2 Frontend 单元/组件

当前项目没有组件测试框架。实施时引入与 Vue 3/Vite 兼容的 Vitest、Vue Test Utils 和 DOM 环境，仅用于本仓库测试：

- primitive 属性转发和键盘行为；
- FormField ARIA 关联和字段错误；
- formatter 对 0/false/null/invalid/unknown 的处理；
- URL query encode/decode；
- 旧请求晚返回时不覆盖新状态；
- DataTable 排序、选择范围、分页和行菜单；
- feedback 层级和 confirm queue；
- Drawer 关闭后焦点恢复。

### 8.3 浏览器

- 使用真实管理员登录态遍历全部 15 个路由；
- 对每页执行至少一次搜索/筛选、分页/选择、打开详情和主要表单；
- 检查成功、字段失败、提交失败、加载失败和危险确认；
- 检查 URL 刷新/前进/后退；
- 1440、1024、768、390px 视口；
- 键盘-only 和 ARIA 状态；
- 页面没有浏览器原生 JS dialog、意外横向溢出或不可达操作。

已在有效管理员登录态完成 15 个管理路由的桌面语义、溢出、表格 caption、状态图标标签和原生控件标签矩阵，
并完成登录/公共外壳 1024、768、390px 响应式检查。管理端多视口、详情抽屉键盘循环、字段错误辅助技术和
完整浏览器历史交互仍需补齐，不能用源码或 build 代替。

## 9. 性能与观测

- 后端测试输出每个样板列表的 SQL 数量和耗时；
- 内网验证记录 API p50/p95、响应体大小和首屏可操作时间；
- 前端开发模式可记录请求 generation、取消和任务 ID，但不得记录秘密、完整订阅 URL 或私钥；
- 长列表 DOM 行数断言不超过当前页大小；
- 聚合接口与列表接口分别观察，避免统计卡拖慢主列表。

## 10. 每阶段完成门槛

一个阶段只有同时满足以下条件才可标记完成：

1. 计划内代码和兼容清理完成；
2. 新增测试覆盖该阶段目标且通过；
3. `go test ./...`、`go vet ./...`、`pnpm build`、`git diff --check` 通过；
4. 真实浏览器完成该阶段路由、键盘、ARIA 和目标宽度交互；
5. 没有把剩余页面或旧组件问题隐藏为“后续优化”；
6. 记录当前证据和剩余缺口；
7. 只有完整实现目标达到仓库要求时才更新 `PROJECT_MEMORY.md` 并执行内网同步和部署后验收。

## 11. 已通过的产品门槛

以下取舍已于 2026-07-22 确认，阶段 A 及后续迁移均以此为准：

- Nodes 默认全宽表格 + 详情抽屉，并允许固定分栏；
- Protocols 默认紧凑表格，按节点分组为可选视图；
- 默认 40px/50 条；
- 查看与编辑分离；
- 全部工作台状态写入 URL；
- 长操作统一任务化；
- Dashboard 只保留可行动指标。
- 状态使用“语义图标 + 文字”的标签，不能只显示裸文字或仅靠颜色表达；
- 数量在表格中直接显示数字，单位和口径写在列标题或字段标签中；
- 时间显示格式化标签，近期时间优先相对表达，并通过标题或详情保留精确时间。

产品确认门槛已结束。实施必须先完成公共契约，再以 Nodes/Protocols 验证规模化方案，随后迁移全部管理端页面，不能把样板完成视为整体完成。
