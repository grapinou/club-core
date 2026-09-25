-- +goose Up
ALTER TABLE organizations
 ADD COLUMN public_phone_label TEXT,
 ADD COLUMN trial_session_description TEXT,
 ADD COLUMN trial_items_to_bring TEXT[] NOT NULL DEFAULT '{}',
 ADD COLUMN trial_equipment_offer TEXT,
 ADD COLUMN trial_equipment_detail_prompt TEXT;

CREATE TABLE organization_public_images (
 organization_id INTEGER NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
 placement TEXT NOT NULL CHECK (placement IN ('hero','activity','community','schedule','trial')),
 src TEXT NOT NULL CHECK (src LIKE '/static/%'),
 webp_srcset TEXT NOT NULL DEFAULT '',
 alt TEXT NOT NULL DEFAULT '',
 width INTEGER NOT NULL CHECK (width > 0),
 height INTEGER NOT NULL CHECK (height > 0),
 PRIMARY KEY (organization_id, placement)
);

-- +goose Down
DROP TABLE organization_public_images;
ALTER TABLE organizations
 DROP COLUMN public_phone_label,
 DROP COLUMN trial_session_description,
 DROP COLUMN trial_items_to_bring,
 DROP COLUMN trial_equipment_offer,
 DROP COLUMN trial_equipment_detail_prompt;
