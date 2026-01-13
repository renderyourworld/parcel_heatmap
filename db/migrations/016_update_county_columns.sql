-- Remove unused BBox column
ALTER TABLE counties DROP COLUMN IF EXISTS b_box;

-- Add IsGovernmentWindow column
ALTER TABLE counties ADD COLUMN is_government_window boolean DEFAULT false;