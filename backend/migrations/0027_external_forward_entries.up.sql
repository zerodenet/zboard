ALTER TABLE `network_entries`
 DROP FOREIGN KEY `fk_network_entries_entry_node`,
 ADD COLUMN `deployment_mode` varchar(16) NOT NULL DEFAULT 'managed',
 MODIFY COLUMN `node_id` bigint unsigned DEFAULT NULL,
 ADD CONSTRAINT `fk_network_entries_entry_node_nullable` FOREIGN KEY (`node_id`) REFERENCES `nodes` (`id`) ON DELETE RESTRICT;
