DELETE FROM `system_configs`
WHERE `config_key` IN (
  'site_logo_dark',
  'site_favicon',
  'site_footer_copyright',
  'site_support_email',
  'site_support_url',
  'site_telegram_url',
  'site_terms_url',
  'site_privacy_url',
  'site_refund_url',
  'site_legal_items',
  'site_meta_title',
  'site_meta_description',
  'site_home_kicker',
  'site_home_title',
  'site_home_primary_cta'
);