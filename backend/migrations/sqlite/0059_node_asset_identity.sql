-- Owned initialization is implemented by reconcileNodeAssetSchema.
ALTER TABLE nodes ADD COLUMN asset_tag TEXT NOT NULL DEFAULT '';
