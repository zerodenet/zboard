INSERT IGNORE INTO `system_configs`
  (`config_key`, `name`, `value`, `value_type`, `description`, `is_public`, `is_secret`, `revision`, `created_at`, `updated_at`)
VALUES
  ('site_logo_dark', '深色站点 Logo', '', 'string', '深色背景使用的 Logo URL；为空时回退到站点 Logo。', 1, 0, 1, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  ('site_favicon', '站点图标', '', 'string', '浏览器标签页和收藏夹使用的 favicon URL。', 1, 0, 1, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  ('site_footer_copyright', '页脚版权', '', 'string', '公开站点页脚显示的版权文本；为空时自动使用站点名称和当前年份。', 1, 0, 1, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  ('site_support_email', '客服邮箱', '', 'string', '公开站点用于联系支持团队的邮箱地址。', 1, 0, 1, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  ('site_support_url', '客服入口', '', 'string', '公开站点的客服、工单或帮助中心 URL。', 1, 0, 1, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  ('site_telegram_url', 'Telegram 社区', '', 'string', '公开站点展示的 Telegram 群组或频道 URL。', 1, 0, 1, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  ('site_terms_url', '服务条款', '', 'string', '服务条款页面 URL；为空时不展示。', 1, 0, 1, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  ('site_privacy_url', '隐私政策', '', 'string', '隐私政策页面 URL；为空时不展示。', 1, 0, 1, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  ('site_refund_url', '退款政策', '', 'string', '退款或取消政策页面 URL；为空时不展示。', 1, 0, 1, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  ('site_legal_items', '法律与注册信息', '[]', 'json', '可选的地区中立法律或注册信息数组。每项支持 label、value 和可选 url，例如 Company No.、VAT 或当地备案信息。', 1, 0, 1, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  ('site_meta_title', 'SEO 标题', '', 'string', '浏览器标题和基础 SEO 标题；为空时使用站点名称。', 1, 0, 1, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  ('site_meta_description', 'SEO 描述', '', 'string', '页面 meta description；为空时使用站点描述。', 1, 0, 1, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  ('site_home_kicker', '首页提示语', '灵活套餐 · 独立订阅 · 清晰计费', 'string', '首页主标题上方的短提示语。', 1, 0, 1, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  ('site_home_title', '首页主标题', '选择适合你的套餐，按需订阅，轻松管理。', 'string', '首页 Hero 区域的主标题。', 1, 0, 1, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  ('site_home_primary_cta', '首页主按钮', '浏览套餐', 'string', '首页 Hero 区域主按钮文本，目标固定为套餐页。', 1, 0, 1, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3));