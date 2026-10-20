INSERT INTO system_configs(config_key,name,value,value_type,description,is_public,is_secret,revision,created_at,updated_at)
VALUES('subscription_renewal_grace_days','订阅续费恢复窗口（天）','7','int','到期后停服，可在此天数内续费恢复；默认7天，范围0–3650，0表示不保留恢复窗口。缩短窗口会使超过新期限的旧订阅结束并清理凭据，历史订单和账务保留。主动退订不进入恢复窗口。',0,0,1,NOW(3),NOW(3))
ON DUPLICATE KEY UPDATE config_key=VALUES(config_key);
