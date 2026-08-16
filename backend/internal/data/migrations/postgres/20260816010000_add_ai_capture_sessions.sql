-- +goose Up
CREATE TABLE IF NOT EXISTS "ai_capture_sessions" (
    "id" uuid NOT NULL,
    "created_at" timestamptz NOT NULL,
    "updated_at" timestamptz NOT NULL,
    "location_id" uuid NULL,
    "location_name_snapshot" character varying NOT NULL,
    "status" character varying NOT NULL DEFAULT 'capturing'
        CHECK ("status" IN ('capturing', 'queued', 'analyzing', 'analysis_failed', 'ready_for_review', 'submitting', 'completed')),
    "draft_json" text NULL,
    "draft_revision" bigint NOT NULL DEFAULT 0,
    "analysis_attempts" bigint NOT NULL DEFAULT 0,
    "photo_count" bigint NOT NULL DEFAULT 0,
    "worker_lease_until" timestamptz NULL,
    "error_code" character varying NULL CHECK ("error_code" IS NULL OR char_length("error_code") <= 64),
    "error_message" character varying NULL CHECK ("error_message" IS NULL OR char_length("error_message") <= 1000),
    "finished_at" timestamptz NULL,
    "analyzed_at" timestamptz NULL,
    "completed_at" timestamptz NULL,
    "expires_at" timestamptz NOT NULL,
    "group_id" uuid NOT NULL,
    "user_id" uuid NOT NULL,
    PRIMARY KEY ("id"),
    CONSTRAINT "ai_capture_sessions_groups_ai_capture_sessions" FOREIGN KEY ("group_id") REFERENCES "groups" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
    CONSTRAINT "ai_capture_sessions_users_ai_capture_sessions" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
    CONSTRAINT "ai_capture_sessions_entities_ai_capture_sessions" FOREIGN KEY ("location_id") REFERENCES "entities" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS "aicapturesession_user_id_updated_at" ON "ai_capture_sessions" ("user_id", "updated_at");
CREATE INDEX IF NOT EXISTS "aicapturesession_group_id_user_id_status" ON "ai_capture_sessions" ("group_id", "user_id", "status");
CREATE INDEX IF NOT EXISTS "aicapturesession_status_worker_lease_until" ON "ai_capture_sessions" ("status", "worker_lease_until");

CREATE TABLE IF NOT EXISTS "ai_capture_photos" (
    "id" uuid NOT NULL,
    "created_at" timestamptz NOT NULL,
    "updated_at" timestamptz NOT NULL,
    "session_id" uuid NOT NULL,
    "client_photo_id" uuid NOT NULL,
    "position" bigint NOT NULL CHECK ("position" >= 0),
    "original_name" character varying NOT NULL CHECK (char_length("original_name") <= 255),
    "path" character varying NOT NULL,
    "mime_type" character varying NOT NULL CHECK (char_length("mime_type") <= 100),
    "size_bytes" bigint NOT NULL CHECK ("size_bytes" >= 0),
    "content_hash" character varying NOT NULL CHECK (char_length("content_hash") <= 128),
    PRIMARY KEY ("id"),
    CONSTRAINT "ai_capture_photos_ai_capture_sessions_photos" FOREIGN KEY ("session_id") REFERENCES "ai_capture_sessions" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS "aicapturephoto_session_id_client_photo_id" ON "ai_capture_photos" ("session_id", "client_photo_id");
CREATE UNIQUE INDEX IF NOT EXISTS "aicapturephoto_session_id_position" ON "ai_capture_photos" ("session_id", "position");

CREATE TABLE IF NOT EXISTS "ai_capture_session_items" (
    "id" uuid NOT NULL,
    "created_at" timestamptz NOT NULL,
    "updated_at" timestamptz NOT NULL,
    "session_id" uuid NOT NULL,
    "client_id" character varying NOT NULL CHECK (char_length("client_id") <= 255),
    "entity_id" uuid NULL,
    "status" character varying NOT NULL DEFAULT 'pending'
        CHECK ("status" IN ('pending', 'creating', 'attaching', 'completed', 'failed')),
    "uploaded_photo_ids" text NOT NULL DEFAULT '[]',
    "error_code" character varying NULL CHECK ("error_code" IS NULL OR char_length("error_code") <= 64),
    PRIMARY KEY ("id"),
    CONSTRAINT "ai_capture_session_items_ai_capture_sessions_items" FOREIGN KEY ("session_id") REFERENCES "ai_capture_sessions" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS "aicapturesessionitem_session_id_client_id" ON "ai_capture_session_items" ("session_id", "client_id");

-- +goose Down
DROP TABLE IF EXISTS "ai_capture_session_items";
DROP TABLE IF EXISTS "ai_capture_photos";
DROP TABLE IF EXISTS "ai_capture_sessions";
