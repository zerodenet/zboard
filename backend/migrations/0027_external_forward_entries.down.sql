-- External entries must be removed before rolling back to managed-only storage.
ALTER TABLE `network_entries`
 DROP FOREIGN KEY `fk_network_entries_entry_node_nullable`,
 MODIFY COLUMN `node_id` bigint unsigned NOT NULL,
 DROP COLUMN `deployment_mode`,
 ADD CONSTRAINT `fk_network_entries_entry_node` FOREIGN KEY (`node_id`) REFERENCES `nodes` (`id`) ON DELETE RESTRICT;
