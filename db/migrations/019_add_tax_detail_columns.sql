-- Add detail columns to parcel_taxes table for GovWindow scraper data
ALTER TABLE parcel_taxes ADD COLUMN IF NOT EXISTS bill_url text;
ALTER TABLE parcel_taxes ADD COLUMN IF NOT EXISTS building_value numeric;
ALTER TABLE parcel_taxes ADD COLUMN IF NOT EXISTS land_value numeric;
ALTER TABLE parcel_taxes ADD COLUMN IF NOT EXISTS due_date varchar(20);
ALTER TABLE parcel_taxes ADD COLUMN IF NOT EXISTS paid_date varchar(20);
ALTER TABLE parcel_taxes ADD COLUMN IF NOT EXISTS total_due numeric;
ALTER TABLE parcel_taxes ADD COLUMN IF NOT EXISTS back_taxes numeric;
ALTER TABLE parcel_taxes ADD COLUMN IF NOT EXISTS last_updated_date timestamp;
