-- +goose Up
create table if not exists ai_capture_sessions
(
    id                     uuid                                not null primary key,
    created_at             datetime                            not null,
    updated_at             datetime                            not null,
    location_id            uuid,
    location_name_snapshot text                                not null,
    status                 text default 'capturing'            not null
        check (status in ('capturing', 'queued', 'analyzing', 'analysis_failed', 'ready_for_review', 'submitting', 'completed')),
    draft_json             text,
    draft_revision         integer default 0                   not null,
    analysis_attempts      integer default 0                   not null,
    photo_count            integer default 0                   not null,
    worker_lease_until     datetime,
    error_code             text check (error_code is null or length(error_code) <= 64),
    error_message          text check (error_message is null or length(error_message) <= 1000),
    finished_at            datetime,
    analyzed_at            datetime,
    completed_at           datetime,
    expires_at             datetime                            not null,
    group_id               uuid                                not null
        constraint ai_capture_sessions_groups_ai_capture_sessions references groups on delete cascade,
    user_id                uuid                                not null
        constraint ai_capture_sessions_users_ai_capture_sessions references users on delete cascade,
    constraint ai_capture_sessions_entities_ai_capture_sessions
        foreign key (location_id) references entities on delete set null
);

create index if not exists aicapturesession_user_id_updated_at
    on ai_capture_sessions (user_id, updated_at);
create index if not exists aicapturesession_group_id_user_id_status
    on ai_capture_sessions (group_id, user_id, status);
create index if not exists aicapturesession_status_worker_lease_until
    on ai_capture_sessions (status, worker_lease_until);

create table if not exists ai_capture_photos
(
    id              uuid     not null primary key,
    created_at      datetime not null,
    updated_at      datetime not null,
    session_id      uuid     not null
        constraint ai_capture_photos_ai_capture_sessions_photos references ai_capture_sessions on delete cascade,
    client_photo_id uuid     not null,
    position        integer  not null check (position >= 0),
    original_name   text     not null check (length(original_name) <= 255),
    path            text     not null,
    mime_type       text     not null check (length(mime_type) <= 100),
    size_bytes      integer  not null check (size_bytes >= 0),
    content_hash    text     not null check (length(content_hash) <= 128)
);

create unique index if not exists aicapturephoto_session_id_client_photo_id
    on ai_capture_photos (session_id, client_photo_id);
create unique index if not exists aicapturephoto_session_id_position
    on ai_capture_photos (session_id, position);

create table if not exists ai_capture_session_items
(
    id                 uuid                      not null primary key,
    created_at         datetime                  not null,
    updated_at         datetime                  not null,
    session_id         uuid                      not null
        constraint ai_capture_session_items_ai_capture_sessions_items references ai_capture_sessions on delete cascade,
    client_id          text                      not null check (length(client_id) <= 255),
    entity_id          uuid,
    status             text default 'pending'    not null
        check (status in ('pending', 'creating', 'attaching', 'completed', 'failed')),
    uploaded_photo_ids text default '[]'          not null,
    error_code         text check (error_code is null or length(error_code) <= 64)
);

create unique index if not exists aicapturesessionitem_session_id_client_id
    on ai_capture_session_items (session_id, client_id);

-- +goose Down
drop table if exists ai_capture_session_items;
drop table if exists ai_capture_photos;
drop table if exists ai_capture_sessions;
