# ZBoard UI/UX Standard v2

> 目标：保留 ZBoard 现有业务能力、信息架构和核心页面布局，彻底重做视觉与交互表现层。以 **shadcn-vue** 作为基础组件体系，以 **Stripe Dashboard** 的设计纪律作为交互与视觉质量基准。

## 1. 总目标

新版 ZBoard 的 UI/UX 目标可以概括为：

> **业务复杂，界面安静；数据密集，操作明确；组件统一，交互可预测。**

新版不追求“强设计感”“酷炫”“玻璃效果”“大面积品牌色”，而追求一种成熟管理后台应有的专业性：用户能够快速理解当前状态、完成操作，并尽可能少地意识到 UI 本身的存在。

Stripe 是设计质量基准，不是品牌视觉模板；shadcn-vue 是统一交互与视觉的实现基础，不是简单换皮。

### 1.1 信息与设备原则

- 优先用布局、层级和控件状态表达结果。已有清晰视觉表达时，不再追加重复标签或解释段落。
- 桌面端以信息清晰、操作集中为主，充分利用实际内容宽度；常用操作就近提供。
- 移动端以触控交互、少输入为主，复用已知信息和合理默认值；不要把桌面表格与说明堆成长页面。
- 页面与弹窗先呈现当前任务及必要操作。实现机制、内部术语和恢复规则不进入默认操作界面。
- 保留必要的字段名称、无障碍名称、错误原因和操作后果；仅在相关状态或决策时出现，措辞简短。

### 1.2 菜单与页面命名

- 四个业务分区内直接展示实际页面名称；不要把抽象分组作为跳转到某个子页面的入口。
- 同一页面的侧栏、搜索结果、面包屑、页签和主标题使用同一名称；详情页使用具体对象名称。
- 名称表达管理员要管理的对象，例如“套餐管理”“订单管理”“订阅模板”；避免“交易管理”“配置交付”等不能明确指出目标页面的入口名称。
- 菜单清单、顺序、可见性与自定义名称仍由后端下发，插件页面遵循同一规则。

### 1.3 整页层级与注意力

- 每种页面先确定核心结果与下一步操作。概览突出当前业务结果，列表突出对象与异常，详情突出当前对象和操作，设置突出字段与保存；不混用促销卡片和后台指标布局。
- 页面标题 24px、分区标题 15–16px、对象名称 13–16px、正文 13–14px、辅助信息 12px。核心金额或流量可以使用 24–30px，其余统计降一级，不能所有数字等大。
- 时间默认是普通辅助文字。正常、成功和普通信息状态使用轻量表达；失败、待核验、到期等真正需要处理的异常才使用明显强调。保留语义标签和精确时间的无障碍名称。
- 独立模块保留清晰的容器边界：页面底色、内容容器和模块内的主次信息必须可辨认。使用一层轻边界与合理内边距分组；不要把减线理解为取消分区，也不叠加外框、标题线、正文线和页脚线。密集表格保留必要的轻分隔；选择卡片、输入控件、浮层和选中状态保留明确边界。
- 统计使用同一指标条，默认无装饰图标和卡片框。详情属性使用同一阅读网格，不把每个时间、编号和状态做成独立小卡片。
- 默认列表工具栏保持虚线胶囊筛选；刷新保留已读内容和稳定布局，后台轮询不能插入改变高度的可见状态。
- 主要操作按当前任务确定。套餐集合中的“查看详情”属于选择入口；进入具体套餐后才突出确认操作。列表行的辅助操作统一进入低权重按钮或菜单。

### 1.4 用户中心

- 以当前套餐、可用流量和就近操作为主；获取订阅、续费和流量重置必须绑定具体订阅，并按实际能力显示。
- 桌面集中展示，手机使用可点按的套餐与订单行；全部明细有直接入口。辅助计数不能与剩余流量或应付金额争抢重点。
- 余额、订阅、订单、套餐和账户设置沿用同一信息层级。金额、付款、续费、退订的必要后果保持可见，技术标识按需展开。
- 业务名称作为主要入口，编号作为辅助。客户端格式选择与链接复制就近展示；套餐详情只保留一个主标题。
- 验收按页面家族覆盖后台和用户中心：概览、列表、详情、设置、选择与确认流程；覆盖桌面、手机、暗色，以及加载、刷新、空数据、错误和权限状态。静态规范检查和组件复用不能替代视觉验收。

---

## 2. 重构边界

### 2.1 保留

- 现有业务能力
- 现有信息架构
- 首页结构
- 各能力模块的主要页面布局
- 已经成熟的业务流程
- API、权限、状态和数据模型
- 已经合理的交互行为，例如弹窗关闭后的焦点恢复、未保存变更确认等

### 2.2 重做

以下内容全部进入新版 UI 重构范围：

- Button
- Input
- Textarea
- Select
- Combobox
- Checkbox
- Radio
- Switch
- Tabs
- Badge
- Tooltip
- Dropdown Menu
- Context Menu
- Popover
- Command
- Dialog
- Alert Dialog
- Sheet / Drawer
- Toast
- Alert
- Skeleton
- Pagination
- Table / Data Table
- Filter Bar
- Form Field
- Empty State
- Loading State
- Error State
- Focus / Hover / Active / Disabled 状态
- 页面标题区
- Sidebar / Topbar 的视觉与交互
- 全局 spacing / radius / shadow / typography / motion

### 2.3 禁止的“伪重构”

以下做法不视为完成新版重构：

```text
UiButton.vue
  ↓
PrimeButton
  ↓
用 CSS 模仿 shadcn
```

也不接受：

```text
<button class="看起来像 shadcn">
```

目标是建立真正统一的 shadcn-vue / Reka UI 组件体系，而不是继续在 PrimeVue 上叠加新的外观层。

---

## 3. 组件架构

前端组件分为四层：

| 层级 | 建议目录 | 职责 | 示例 |
|---|---|---|---|
| Primitive | `components/ui` | shadcn-vue 原子组件 | Button、Input、Dialog、DropdownMenu |
| Pattern | `components/patterns` | 通用后台交互组合 | DataTable、FilterBar、PageHeader、FormField |
| Domain | `components/domain` | ZBoard 业务组件 | NodeStatus、TrafficUsage、ProtocolStatus |
| Page | `views` | 页面编排和业务逻辑 | Nodes、Plans、Orders |

### 规则

- Page 层禁止重新设计 Primitive。
- 同一个交互如果在两个以上页面出现，应下沉至 Pattern 或 Domain。
- 通用交互组件不得按页面复制。
- 页面级 CSS 只能解决布局和业务可视化，不能改写基础控件视觉。

---

## 4. 设计语言

### 4.1 视觉原则

整体采用：

> **Neutral UI + restrained brand accent**

即：90% 的后台界面由中性色构成，品牌色仅用于关键操作、选中状态和必要强调。

### 4.2 色彩 Token

页面禁止直接使用业务无关的具体色值，例如：

```css
color: #6c5ce7;
background: #f3f1ff;
```

统一使用语义 Token：

| Token | 用途 |
|---|---|
| `background` | 页面背景 |
| `foreground` | 主文字 |
| `card` | 必要容器 |
| `muted` | 次级区域 |
| `muted-foreground` | 次要文字 |
| `border` | 普通边界 |
| `input` | 输入框边界 |
| `primary` | 品牌主操作 |
| `primary-foreground` | 主按钮文字 |
| `destructive` | 危险操作 |
| `success` | 成功状态 |
| `warning` | 警告状态 |
| `info` | 信息状态 |
| `ring` | Focus Ring |

品牌色不得默认用于普通标题、普通图标、普通表格文本。

---

## 5. 圆角与阴影

### 5.1 圆角

| 类型 | 建议 |
|---|---:|
| Button / Input | 6px |
| Badge | 6px；pill 仅在明确语义场景使用 |
| Dropdown / Popover | 8px |
| Dialog / Sheet | 10px |
| Card | 10px |

同一页面不得出现大量不一致的圆角体系。

### 5.2 阴影

原则：

> **先组织内容关系，再使用必要边界；阴影只辅助浮层。**

普通 Card 不使用明显阴影；Dropdown、Popover、Dialog 等浮层允许使用克制的阴影。

禁止：

- 大面积 glow
- 品牌色阴影
- Card hover 时明显抬升
- 强投影作为主要层级手段

---

## 6. 尺寸与密度

ZBoard 是管理后台，默认采用偏紧凑 Desktop Density。

| 元素 | 标准尺寸 |
|---|---:|
| Compact control | 32px |
| Default control | 36px |
| Large control | 40px |
| Table row | 44px |
| Compact table row | 36–40px |
| Icon button | 32 / 36px |
| Sidebar item | 36–40px |

后台绝大多数按钮应使用 32px 或 36px 高度。40px 以上主要用于登录、注册、公开购买等场景。

---

## 7. Typography

推荐字体：

```css
font-family:
  Inter,
  ui-sans-serif,
  system-ui,
  -apple-system,
  "PingFang SC",
  "Microsoft YaHei",
  sans-serif;
```

建议字号：

| 内容 | 大小 |
|---|---:|
| Page title | 24px |
| Section title | 16px |
| Card title | 14px |
| Body | 14px |
| Table | 13px |
| Secondary | 12px |
| Metadata | 11–12px |

主要字重：400 / 500 / 600。

避免大面积 700 / 800。粗体不应成为默认强调手段。

---

## 8. 页面布局 Pattern

### 8.1 Data Page

适用于：节点、订单、用户、套餐、协议等。

```text
PageHeader
DataToolbar
DataTable
Pagination
Detail Sheet
```

避免默认使用：

```text
Page
└── 巨型 Card
    ├── Toolbar
    ├── Table
    └── Footer
```

Table 本身就是页面主体，不应为了“有层级”而强制套大白卡。

### 8.2 Settings Page

```text
PageHeader

Section
  title
  description
  fields
  action
```

不要每几个字段就包一个 Card。优先使用 Section、Divider 和自然留白。

### 8.3 Detail Page

```text
Heading
Status / Metadata
Tabs
Content Sections
```

列表快速查看详情优先使用 Sheet；复杂管理任务可跳转完整详情页。

---

## 9. Page Header

后台标题区应保持克制：

```text
Title                         Actions
Description
```

Eyebrow 可以使用，但不能成为所有页面强制元素。

推荐：

```text
商品与套餐                     创建商品
管理商品、SKU 与销售状态
```

而不是：

```text
CATALOG
商品与套餐
列表只保留商品摘要……
```

标题区与主体间距建议 16–24px。普通后台页面标题区不应占用过高垂直空间。

---

## 10. Button

统一使用 shadcn Button。

允许 Variant：

```text
default
secondary
outline
ghost
destructive
link
```

不额外扩展大量项目专属 variant。

| 操作 | Variant |
|---|---|
| 页面核心动作 | Default |
| 普通操作 | Secondary / Outline |
| 表格操作 | Ghost |
| 危险操作 | Destructive |
| 文字导航 | Link |

一个操作区原则上只有一个 Primary。

例如：

```text
[刷新] [创建节点]
```

推荐处理为：

```text
Refresh icon      + 创建节点
Ghost/Outline       Primary
```

---

## 11. Icon Button

所有纯图标操作必须：

- 有 Tooltip
- 有 `aria-label`
- 使用统一点击面积
- 有 hover
- 有 focus-visible

推荐标准尺寸：32 × 32。

表格低频操作优先进入 `DropdownMenu`：

```text
查看      ⋯
```

不要将所有低频操作横向铺开。

---

## 12. Data Table

DataTable 是新版后台最核心的 Pattern 之一。

| 内容 | 标准 |
|---|---|
| Header | 低对比、中性色 |
| Row | 默认无斑马纹 |
| Hover | 极浅 neutral |
| Border | 只做必要分隔 |
| Numeric | 右对齐 |
| Status | Badge |
| Time | 相对时间或 compact timestamp |
| Action | 最右侧 |
| Selection | 最左侧 |
| Empty | Table 内 EmptyState |
| Loading | Skeleton Rows |

禁止：

- 一个 Cell 塞大量 Badge
- 操作列堆叠多个按钮
- 表格内部使用高饱和品牌色吸引注意力

---

## 13. Filter System

本项目已确认的列表筛选风格是 **虚线胶囊入口**：搜索、状态等条件使用同一筛选语言；选中后展示条件值与清除入口。

- 搜索输入框和提交按钮在展开后的筛选浮层中出现，不在默认列表工具栏中常驻。
- 胶囊是筛选条件的专用 Pattern，不能直接替换成普通矩形 Button，也不能用普通 Button 的尺寸和圆角规则覆盖它。
- 组件复用与尺寸统一不构成符合 Stripe 风格的验收结论，仍须检查整个筛选栏的组合及各交互状态。

新版筛选不再默认采用：

```text
Input + Select + Select + Select + Button
```

推荐：

```text
Search
+ Status
+ Region
+ Protocol
```

点击条件后通过 Popover / Command 选择：

```text
Status
✓ Running
  Disabled
  Error
```

选中后显示：

```text
Status: Running ×
```

FilterBar 应统一支持：

- Clear all
- active count
- URL query persistence
- 键盘操作
- 空条件时保持干净

---

## 14. Dialog / Sheet / Popover 使用边界

| 场景 | 组件 |
|---|---|
| 小型创建/编辑 | Dialog |
| 危险确认 | AlertDialog |
| 复杂详情 | Sheet |
| 长表单 | Sheet / Page |
| 快速选择 | Popover |
| 低频操作 | DropdownMenu |
| 简短补充说明 | Tooltip |

不要所有事情都放 Dialog，也不要所有事情都放 Drawer。

---

## 15. Form

所有字段统一使用一个 Pattern：

```text
FormField
 ├─ Label
 ├─ Control
 ├─ Description
 └─ Error
```

字段错误必须出现在字段附近。

错误示例：

```text
Toast: 邮箱地址格式错误
```

正确示例：

```text
邮箱
[ invalid mail ]
请输入有效邮箱地址
```

Toast 不承担表单校验职责。

---

## 16. Loading

统一三种 Loading：

### 16.1 按钮操作

```text
[ spinner 保存中 ]
```

按钮宽度尽量保持稳定。

### 16.2 数据首次加载

使用 Skeleton。

### 16.3 数据刷新

刷新时保留现有数据，不让整个区域消失后再重新出现。

禁止：

```text
表格消失
↓
大 Spinner
↓
表格回来
```

---

## 17. Feedback

| 场景 | 方式 |
|---|---|
| 操作成功 | Toast |
| 普通失败 | Toast / Inline Alert |
| 字段问题 | Inline Error |
| 页面级问题 | Alert |
| 高风险确认 | AlertDialog |

成功 Toast 保持简短：

```text
节点已更新
```

避免：

```text
节点更新操作已经成功完成！
```

---

## 18. Status Badge

Badge 是状态表达，不是装饰。

统一语义：

```text
neutral
success
warning
destructive
info
```

例如：

```text
● 在线
● 待验证
● 已停用
```

颜色只作为辅助，文字必须能够独立表达状态。

---

## 19. Motion

后台动效应尽量不被用户明显感知。

推荐：

```text
100–200ms
ease-out
```

适用于：

- hover
- popover
- dropdown
- sheet
- dialog

禁止：

- bounce
- scale 1.05
- 大幅 slide
- 按钮抖动
- Card 上浮

原则：

> **界面不会乱动。**

---

## 20. Focus / Keyboard / Accessibility

所有交互组件必须支持：

```text
Tab
Shift+Tab
Enter
Space
Escape
Arrow Keys
```

必须始终存在 `focus-visible`。

禁止：

```css
outline: none;
```

除非已经提供等价且明确的可见焦点样式。

Dialog / Sheet 必须正确处理：

- focus trap
- Escape
- return focus

---

## 21. Sidebar

Sidebar 目标：

```text
低噪音
紧凑
图标统一
Active 状态明确但克制
```

避免：

- 大块品牌色背景
- 强阴影
- 粗竖条 + 背景 + 品牌色文字同时叠加

Active 状态通常只需要：

```text
subtle background
+
foreground emphasis
```

二级导航保持同一交互语言。

---

## 22. Page Background 与 Card

核心原则：

> **不要为了分区而使用 Card。**

只有真正独立的内容实体才适合使用 Card，例如：

- 套餐
- 支付方式
- 统计指标
- 插件

以下内容通常不需要 Card：

- Table
- 普通 Form
- Settings Section
- Page Toolbar
- Pagination

避免“灰色大背景 + 巨型白 Card”的默认后台模板感。

---

## 23. CSS 纪律

页面禁止通过局部 CSS 修改基础组件，例如：

```css
.my-page .p-button { ... }
.my-dialog button { ... }
.special-input { ... }
```

页面 CSS 仅允许用于：

- Layout
- Domain visualization
- Special data presentation

不得改变以下基础元素的标准视觉：

- Button
- Input
- Select
- Dialog
- Dropdown
- Table primitive

---

## 24. 禁止事项

| 禁止 | 原因 |
|---|---|
| 页面自行定义按钮 | 破坏统一 |
| 页面自行设计 Input | 状态不一致 |
| 大量 Card 嵌套 | 增加视觉噪音 |
| Gradient 用于后台控件 | 不符合目标 |
| Strong shadow | 破坏克制视觉 |
| 大面积品牌紫 | 抢夺内容注意力 |
| Hover scale | 不专业 |
| 成功消息长期常驻 | 增加噪音 |
| 所有操作都要求确认 | 降低效率 |
| 所有操作都平铺成按钮 | 信息拥挤 |
| 过多 Badge | 数据难读 |
| 自定义 Select | 键盘与浮层行为难统一 |
| `window.confirm()` | 交互体系断裂 |

---

## 25. Responsive

后台优先优化：

```text
1280+
1440
1920
```

同时必须正常支持：

```text
1024
768
```

复杂表格在较小屏幕下允许采用：

- responsive column priority
- horizontal scroll
- Sheet
- mobile navigation

禁止通过把字体缩小到不可读的方式硬塞布局。

---

## 26. Dark Mode

设计系统从第一天支持 Dark Token，但 UI 开关可以稍后开放。

组件不得依赖：

```css
background: white;
```

应始终使用语义 Token：

```css
background: var(--background);
```

这样可以避免后续 Dark Mode 二次返工。

---

## 27. 验收标准

不能再用“构建成功”判断 UI 重构完成。

### 27.1 组件状态

每个基础组件至少验证：

- Default
- Hover
- Focus
- Active
- Disabled
- Loading
- Error
- Empty
- Long content
- Small viewport

### 27.2 页面状态

每个页面至少验证：

- 正常数据
- 空数据
- 加载
- 刷新
- API 失败
- 权限不足
- 长文本
- 大量数据

### 27.3 交互一致性

检查：

- 同类操作是否使用同类控件
- 同类状态是否使用一致的视觉语义
- 表单错误是否都在字段附近
- 删除等危险操作是否使用 AlertDialog
- Sheet / Dialog 是否有正确 Focus 行为
- 同一个组件在不同页面是否表现一致

---

## 28. 推荐的工程文档结构

建议在仓库中最终形成：

```text
docs/ui/
├── DESIGN_SYSTEM.md
├── COMPONENT_GUIDELINES.md
└── PAGE_PATTERNS.md
```

### `DESIGN_SYSTEM.md`

负责：

- Color
- Typography
- Spacing
- Radius
- Shadow
- Motion
- Density

### `COMPONENT_GUIDELINES.md`

负责：

- Button
- Form
- Dialog
- Sheet
- Table
- Dropdown
- Toast
- Badge
- Tooltip

### `PAGE_PATTERNS.md`

负责：

- List Page
- Detail Page
- Settings Page
- Dashboard
- CRUD Flows

---

## 29. 第一阶段实施基线

正式开始全站重构前，优先完成：

1. Design Tokens
2. Button
3. Input / Textarea
4. Select / Combobox
5. Checkbox / Radio / Switch
6. Badge
7. Tooltip
8. DropdownMenu
9. Dialog / AlertDialog
10. Sheet
11. Toast / Alert
12. FormField
13. DataTable
14. FilterBar
15. Pagination
16. PageHeader
17. EmptyState / Skeleton
18. 一个完整 Data Page 样板

推荐使用“节点管理”或“商品与套餐”作为第一套完整样板页，因为它们能够同时覆盖：

- PageHeader
- Search
- Filter
- Table
- Status Badge
- Row Action
- Pagination
- Detail Sheet
- Edit Form
- Confirm Dialog
- Loading
- Error
- Empty State

只有样板页达到目标后，再批量迁移其余页面。

---

## 30. 核心判断标准

新版 ZBoard 的目标不是“变得更花哨”，而是：

- 减少设计感，增加系统感
- 减少页面级自由发挥，增加组件一致性
- 减少视觉噪音，增加数据密度
- 减少强品牌色，增加层级清晰度
- 减少自定义交互，增加成熟通用模式
- 保留产品能力，重做界面表达

最终标准：

> **用户首先看到的是数据、状态和操作，而不是 UI 本身。**
