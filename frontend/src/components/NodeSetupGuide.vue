<template>
  <details class="node-setup-guide">
    <summary>接入流程：服务器 → 协议服务 → 节点组 → 商品</summary>
    <ol>
      <li><RouterLink to="/admin/nodes">登记服务器</RouterLink><span>配置 SSH，安装 Zero 并确认连接正常。</span></li>
      <li><RouterLink to="/admin/protocols">创建协议服务</RouterLink><span>选择承载服务器，设置协议、监听端口与对外地址。</span></li>
      <li><RouterLink to="/admin/node-groups">配置节点组</RouterLink><span>将协议服务加入节点组，确定用户可用的节点范围。</span></li>
      <li><RouterLink to="/admin/plans">关联商品</RouterLink><span>选择节点组并配置流量额度和销售规格。</span></li>
    </ol>
  </details>
</template>
<script setup lang="ts">
import { RouterLink } from 'vue-router'
</script>
<style scoped>
.node-setup-guide { border-block: 1px solid var(--line); padding: 12px 0; margin-bottom: 16px; font-size: 12px; }
summary { cursor: pointer; color: var(--muted-foreground); }
summary:focus-visible { outline: 2px solid var(--ring); outline-offset: 2px; }
ol { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 20px; margin: 16px 0 0; padding-left: 20px; }
li { padding-left: 2px; } a { font-weight: 600; } span { display: block; color: var(--muted-foreground); margin-top: 5px; }
@media (max-width: 800px) { ol { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@media (max-width: 480px) { ol { grid-template-columns: 1fr; } }
</style>
