-- +goose Up
ALTER TABLE "ai_capture_sessions"
    ADD COLUMN "capture_revision" bigint NOT NULL DEFAULT 0;

ALTER TABLE "ai_capture_photos"
    ADD COLUMN "capture_group_id" uuid NULL;

-- +goose Down
ALTER TABLE "ai_capture_photos" DROP COLUMN "capture_group_id";
ALTER TABLE "ai_capture_sessions" DROP COLUMN "capture_revision";
