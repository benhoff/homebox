-- +goose Up
alter table ai_capture_sessions
    add column capture_revision integer default 0 not null;

alter table ai_capture_photos
    add column capture_group_id uuid;

-- +goose Down
alter table ai_capture_photos drop column capture_group_id;
alter table ai_capture_sessions drop column capture_revision;
