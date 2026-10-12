// Translate only known host-owned failures. Never display arbitrary runtime,
// database, proxy, or plugin-supplied English text as a public error message.
const messages: Record<string, string> = {
  'invalid or oversized plugin request': '插件管理请求包含不支持的字段或内容过大，请刷新页面后重试。',
  'plugin changed; refresh and retry': '插件状态或配置已更新，请刷新后再操作。',
  'plugin service unavailable': '插件服务暂不可用，请刷新状态后重试。',
  'plugin request timed out': '插件请求超时，请查看操作记录并刷新状态。',
  'admin required': '此操作需要管理员权限，请使用管理员账户。',
  'plugin capability is outside the host admission scope': '插件未通过宿主能力准入，无法执行此操作。',
  'plugin API or runtime is incompatible': '插件接口或运行平台与当前宿主不兼容，无法启用。',
  'plugin is incompatible': '插件与当前宿主不兼容，无法启用。',
  'plugin is incompatible with this host': '插件与当前宿主不兼容，请选择兼容的发布版本。',
  'plugin lifecycle data preparation is incomplete or incompatible': '插件数据准备尚未完成或不兼容，请查看迁移记录并重新导入兼容版本。',
  'plugin version cannot read current data': '插件版本无法读取当前数据，请选择兼容的发布版本。',
  'applied migration checksum changed': '插件迁移校验失败，请检查已安装版本和迁移记录。',
  'duplicate applied migration': '插件迁移记录存在重复，请检查迁移记录。',
  'incomplete migration history': '插件迁移记录不完整，无法启用。',
  'plugin process failed to start': '插件进程启动失败，请查看操作记录后重试。',
  'plugin protocol negotiation failed': '插件进程协议协商失败，请检查插件版本。',
  'plugin identity does not match signed manifest': '插件进程身份与签名清单不一致，无法运行。',
  'runtime requests unsupported capability': '插件进程请求了未通过准入的能力，无法运行。',
  'plugin health check failed': '插件进程健康检查失败，无法启用。',
  'plugin rejected configuration': '插件拒绝了当前配置，请检查配置后重试。',
  'plugin could not apply configuration': '插件进程无法应用当前配置，请检查配置和操作记录。',
  'plugin package missing; import it again': '插件程序包缺失，请重新导入插件。',
  'publisher trust revoked or key changed': '插件发布者的信任已撤销或密钥已变化，请重新核对签名。',
  'unsupported plugin operation': '当前宿主不支持此插件管理操作，请刷新页面。',
  'disable plugin first': '请先停用插件，再清理数据。',
  'import plugin before enabling': '插件已卸载，请重新导入后再启用。',
  'uninstall before deleting configuration': '请先卸载插件，再清除保留数据。',
}

export function pluginManagementErrorMessage(message: string): string {
  return Object.prototype.hasOwnProperty.call(messages, message) ? messages[message] : ''
}
