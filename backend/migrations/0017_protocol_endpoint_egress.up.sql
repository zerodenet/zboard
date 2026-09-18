ALTER TABLE `protocol_endpoints`
    ADD COLUMN `egress_protocol` varchar(32) NOT NULL DEFAULT '' AFTER `server_config`,
    ADD COLUMN `egress_config` text NULL AFTER `egress_protocol`;
