ALTER TABLE publishing_profiles ADD COLUMN current_revision bigint NOT NULL DEFAULT 1;
CREATE TABLE publishing_profile_versions (
 profile_id text NOT NULL REFERENCES publishing_profiles(id) ON DELETE CASCADE,
 revision bigint NOT NULL,
 settings jsonb NOT NULL,
 template_version text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(profile_id,revision)
);
-- Preserve legacy app image snapshots instead of silently pinning them to the
-- possibly already edited profile. Historical order was not recorded; import
-- distinct historical snapshots, then publish the current defaults last.
WITH historical AS (
 SELECT DISTINCT a.profile_id,
  COALESCE(a.config->>'baseImage','') AS base_image,
  COALESCE(a.config->>'builderImage','') AS builder_image,
  COALESCE(a.config->>'webServerImage','') AS webserver_image
 FROM published_applications a JOIN publishing_profiles p ON p.id=a.profile_id
 WHERE ROW(COALESCE(a.config->>'baseImage',''),COALESCE(a.config->>'builderImage',''),COALESCE(a.config->>'webServerImage',''))
 IS DISTINCT FROM ROW(COALESCE(p.settings->>'baseImage',''),COALESCE(p.settings->>'builderImage',''),COALESCE(p.settings->>'webServerImage',''))
), numbered AS (
 SELECT *,row_number() OVER(PARTITION BY profile_id ORDER BY base_image,builder_image,webserver_image) AS revision FROM historical
)
INSERT INTO publishing_profile_versions(profile_id,revision,settings,template_version)
 SELECT n.profile_id,n.revision,p.settings || jsonb_build_object('baseImage',n.base_image,'builderImage',n.builder_image,'webServerImage',n.webserver_image),'1'
 FROM numbered n JOIN publishing_profiles p ON p.id=n.profile_id;
UPDATE publishing_profiles p SET current_revision=COALESCE((SELECT max(v.revision) FROM publishing_profile_versions v WHERE v.profile_id=p.id),0)+1;
INSERT INTO publishing_profile_versions(profile_id,revision,settings,template_version)
 SELECT id,current_revision,settings,'1' FROM publishing_profiles;
ALTER TABLE published_applications ADD COLUMN profile_revision bigint;
UPDATE published_applications a SET profile_revision=v.revision FROM publishing_profile_versions v
 WHERE v.profile_id=a.profile_id AND
 ROW(COALESCE(a.config->>'baseImage',''),COALESCE(a.config->>'builderImage',''),COALESCE(a.config->>'webServerImage',''))=
 ROW(COALESCE(v.settings->>'baseImage',''),COALESCE(v.settings->>'builderImage',''),COALESCE(v.settings->>'webServerImage',''));
ALTER TABLE published_applications ADD CONSTRAINT published_profile_revision_fkey
 FOREIGN KEY(profile_id,profile_revision) REFERENCES publishing_profile_versions(profile_id,revision) DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE published_applications ADD CONSTRAINT published_profile_revision_required CHECK ((profile_id IS NULL AND profile_revision IS NULL) OR (profile_id IS NOT NULL AND profile_revision IS NOT NULL));
