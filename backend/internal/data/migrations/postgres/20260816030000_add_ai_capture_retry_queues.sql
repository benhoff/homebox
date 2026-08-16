-- +goose Up
alter table ai_capture_sessions add column analysis_next_attempt_at timestamptz null;
alter table ai_capture_sessions add column reanalysis_json text null;
alter table ai_capture_sessions add column reanalysis_status character varying null
    check (reanalysis_status is null or char_length(reanalysis_status) <= 32);
alter table ai_capture_sessions add column reanalysis_next_attempt_at timestamptz null;
alter table ai_capture_sessions add column reanalysis_worker_lease_until timestamptz null;

create index if not exists aicapturesession_status_analysis_next_attempt_at
    on ai_capture_sessions (status, analysis_next_attempt_at);
create index if not exists aicapturesession_reanalysis_status_reanalysis_next_attempt_at
    on ai_capture_sessions (reanalysis_status, reanalysis_next_attempt_at);

-- +goose Down
drop index if exists aicapturesession_reanalysis_status_reanalysis_next_attempt_at;
drop index if exists aicapturesession_status_analysis_next_attempt_at;
alter table ai_capture_sessions drop column reanalysis_worker_lease_until;
alter table ai_capture_sessions drop column reanalysis_next_attempt_at;
alter table ai_capture_sessions drop column reanalysis_status;
alter table ai_capture_sessions drop column reanalysis_json;
alter table ai_capture_sessions drop column analysis_next_attempt_at;
