# AI Capture Sessions

| Scope | Status | Last updated |
| --- | --- | --- |
| Durable capture sessions | Implemented | 2026-08-16 |
| Multiple views of the same item | Implemented | 2026-08-16 |

## Summary

AI Capture Sessions separate taking photos from AI analysis and item review. A user selects a location, opens a continuous camera, takes several photos without leaving the camera view, and finishes the capture when ready. HomeBox uploads each photo in the background, analyzes the sealed photo set, and saves an editable draft for later review.

The session is durable. After the server has received the photos, the user may close the page, use another part of HomeBox, or return on another device. Inventory items are not created until the user reviews and submits the AI draft.

An optional **Multiple views of same item** mode lets the user explicitly group several photographs as evidence for one physical item. Grouping is captured at the shutter rather than inferred later. The AI uses all views in a group to produce one draft item, while the default ungrouped capture behavior remains unchanged.

## Problem

The current AI Capture page uses a file input with `capture="environment"`. On phones, every photo leaves the HomeBox page for the system camera and then returns to a photo-management screen. Photos and AI drafts live only in page memory, and the browser uploads the entire batch when the user starts analysis. Reloading or leaving the page loses the unfinished capture.

This makes high-volume capture slow and fragile:

- The camera must be reopened for every photo.
- Photo review interrupts the act of capturing.
- Analysis blocks the current flow instead of becoming work the user can revisit.
- A reload, browser eviction, or navigation can discard an unfinished batch.
- Upload failures are discovered late, when analysis starts.

Even with durable sessions, the AI cannot reliably know whether two similar photographs show the same physical item or two separate items. Multiple views can therefore produce duplicate draft items, while avoiding duplicate suggestions may incorrectly merge genuinely separate items. The photographer knows this relationship at capture time and needs a fast way to preserve that intent.

## Product decisions

1. The user selects a location before starting a session.
2. Taking photos and reviewing detected items are separate modes.
3. The camera remains open after each shutter press.
4. Each photo is stored locally first and uploaded in the background.
5. **Finish Capturing** seals the photo set and queues AI analysis immediately.
6. Analysis runs on the server and survives browser navigation or closure.
7. Review is always explicit. AI analysis never creates inventory items by itself.
8. The first release keeps the existing `HBOX_AI_MAX_PHOTOS` limit for one session. Supporting larger sessions requires a separate cross-batch grouping and deduplication design.
9. Capture sessions are private to the user who created them, even within a shared collection.
10. The existing visual-metadata boundary remains unchanged: AI suggests only names, visible quantities, descriptions, manufacturers, clearly legible model numbers, item types, existing tags, and photo assignments.
11. Multiple-view grouping is opt-in. With grouping off, the existing free-detection behavior remains unchanged and one photo may still contain several detectable items.
12. With grouping on, the first shutter press starts an explicit same-item group. Further photos join that group until the user taps **Next Item** or turns grouping off.
13. Turning grouping off closes the active group but does not remove grouping from photos already captured. Turning it on never silently changes earlier ungrouped photos.
14. A same-item group represents one physical inventory item, not one unit of quantity. AI may still suggest a visible quantity greater than one for a package or set, but it emits one draft row for the group.
15. Grouping changes the AI prompt, request orchestration, and response schema; it does not require model retraining, fine-tuning, or a different configured model.

## Goals

- Let a user take multiple photos without leaving the live camera.
- Give immediate confirmation for every shutter press without opening a preview screen.
- Upload photos without blocking the next photo.
- Preserve unfinished uploads across page reloads on the same device.
- Allow analysis and review to continue independently of the camera page.
- Give users a list of sessions that are capturing, processing, ready for review, failed, or completed.
- Make upload, analysis, correction, and submission retries idempotent.
- Preserve the existing requirement that users approve and edit the AI draft before item creation.
- Let the photographer declare that several views show the same physical item without leaving the camera.
- Prevent duplicate draft items for explicitly grouped views.
- Preserve same-item groups through local recovery, upload, server analysis, correction, and review.

## Non-goals

- Unlimited photos in one AI analysis.
- Video capture or extracting items from video.
- Warranty, purchase, insurance, serial-number, asset-ID, sold, or custom-field inference.
- Real-time AI detection while the camera is open.
- Automatic item creation without review.
- Sharing an unfinished session with other collection members.
- Guaranteed background upload after the browser has been force-closed. Pending local photos resume the next time HomeBox opens on that device.
- A native iOS or Android application.
- Automatically deciding whether ungrouped photos show the same physical item.
- Grouping photographs across different capture sessions or locations.
- Training or fine-tuning an image model from HomeBox capture data.

## User experience

### Entry point and session list

Opening AI Capture shows a **Capture Sessions** page rather than immediately entering the wizard. The primary action is **New Capture**. Each session card shows:

- Location name
- Creation time and last activity time
- Photo count
- Status
- Upload or analysis progress when applicable
- Expiration date for unfinished sessions
- Primary action such as **Resume Capture**, **Review Items**, or **Retry Analysis**
- Overflow actions for delete and, where valid, change location

Completed sessions may remain in the list as lightweight history, but their temporary source photos are removed after item attachments have been created successfully.

### New capture

1. The user selects or scans a HomeBox location.
2. HomeBox creates a server-side session.
3. Before or during capture, the user may enable **Multiple views of same item**. The default is off for every new session.
4. The app requests camera permission and opens the rear-facing camera.
5. If a continuous camera is unavailable, HomeBox offers **Use System Camera** and **Upload Photos** fallbacks with the same grouping controls in the HomeBox view between selections.

### Continuous camera

The live camera is a full-screen, capture-focused interface:

```text
+------------------------------------------------+
| Close       Garage                    Flash     |
| [ Multiple views of same item: ON ]             |
|                                                |
|                                                |
|               LIVE CAMERA                      |
|                                                |
|                                                |
| [last photo]       [ SHUTTER ]     [switch]    |
| Item group 2 • 3 views          [ Next Item ]  |
| 5 of 8      4 uploaded, 1 uploading            |
|             [ Finish Capturing ]                |
+------------------------------------------------+
```

Required behaviors:

- Pressing the shutter never navigates away from the camera.
- The shutter remains usable while previous photos upload, unless local storage is full or the configured session limit has been reached.
- Each photo produces a brief visual confirmation and, where supported and enabled, light haptic feedback.
- The most recent thumbnail, total count, and upload state are visible but do not obscure the preview.
- Tapping the recent thumbnail opens an optional tray where photos can be inspected or deleted. Closing the tray returns to the same live camera stream.
- Camera controls include rear/front switching when multiple cameras exist and torch control only when the browser reports support.
- Navigating away or hiding the page stops the media tracks. Resuming the session asks for the camera again.
- **Finish Capturing** is disabled until at least one photo exists.
- Reaching the photo limit disables the shutter and explains that the session is full.

#### Multiple-view capture mode

The camera exposes a persistent toggle labeled **Multiple views of same item**. It is off by default and its state is visible without opening the photo tray.

When the toggle is off:

- New photos are ungrouped and retain the existing AI free-detection behavior.
- A photo may contain one item, several items, or a room-level scene.
- The AI may correlate evidence across ungrouped photos, but HomeBox makes no identity guarantee.

When the toggle is on:

- The first photo starts a new same-item group with a locally generated stable `captureGroupId`.
- Every subsequent shutter press uses that group until **Next Item** is tapped.
- **Next Item** closes the current group, keeps the toggle on, and makes the next shutter press start another group.
- The UI shows the active group number and view count. A short live-region announcement confirms both after each shutter press.
- Turning the toggle off closes the active group. Turning it back on starts a new group on the next shutter press; it does not resume a closed group automatically.
- The photo limit continues to count photographs, not groups.

The photo tray displays grouped photos together using a number and text label in addition to color. While the session is still `capturing`, the user can move a photo to another same-item group, make it ungrouped, or start a new group. Deleting the last photo in a group removes the empty group. Grouping edits never delete the underlying photo.

The UI must not imply that multiple views increase quantity. For example, four photographs of one drill form one draft item with quantity one unless the visible evidence supports a different quantity.

### Upload queue

On every shutter press:

1. The browser creates a stable `clientPhotoId`.
2. When same-item mode is active, the browser associates the photo with the active stable `captureGroupId`; otherwise the value is null.
3. The image is normalized to JPEG, orientation is applied, metadata is stripped, and dimensions are bounded before storage.
4. The normalized blob, grouping value, and queue record are committed to IndexedDB atomically.
5. The UI confirms the photo and its group.
6. A background queue uploads it to the session with concurrency limited to two requests.
7. A successful server response marks the local record uploaded and allows its local blob to be reclaimed.

Uploads retry automatically with bounded exponential backoff. A failed photo shows a retry badge without closing the camera. Uploading the same `clientPhotoId` more than once returns the original server photo instead of creating a duplicate.

If the page reloads, the same device reconstructs the queue, group membership, closed groups, and active grouping mode from IndexedDB before resuming uploads. The user may keep capturing while temporarily disconnected, subject to available device storage. Cross-device continuation includes only photos and grouping changes already received by the server.

### Finish capturing

When the user taps **Finish Capturing**:

1. HomeBox stops the camera and moves to a compact finishing screen.
2. Any open same-item group is closed locally.
3. Outstanding local uploads and grouping updates continue with progress and retry controls.
4. Once all expected photos and group assignments are confirmed by the server, the client seals the session.
5. The server changes the session to `queued` and returns immediately.
6. The user may go to the session list, start another capture, or wait on the progress screen.
7. A server worker analyzes the session and persists the resulting draft.
8. The session becomes `ready_for_review`, or `analysis_failed` with a user-safe explanation and retry action.

Sealing is idempotent and includes the client's expected photo count and latest capture revision. The server rejects the transition if either value disagrees so that a photo or grouping change cannot be silently omitted.

### Later review

Opening a `ready_for_review` session uses the existing editable item cards and photo-assignment controls. Changes are autosaved to the session with optimistic revision checking. The page makes the AI scope explicit and keeps manual-only metadata out of the AI correction prompt.

Each explicit same-item group initially produces exactly one item card with all of the group's photos assigned. The card shows a **Multiple views** badge and keeps the stable `captureGroupId` in the draft. Ungrouped photos use the existing free-detection behavior and may produce zero, one, or several draft items.

Grouping is guidance rather than an irreversible inventory constraint. During review, the user may split an incorrect grouped suggestion, merge duplicate suggestions, or change photo assignments. These edits affect the draft only; the sealed capture evidence remains available for AI corrections and audit diagnostics. If the AI believes an explicit group contains unrelated objects or cannot reconcile conflicting views, it returns one review-required item with a warning instead of silently creating multiple items.

The user can:

- Edit all suggested visual fields
- Add or remove draft items
- Change photo assignments
- Split or merge AI suggestions when the capture grouping was incorrect
- Ask AI for a correction
- Undo the most recent AI correction
- Leave and resume review later
- Submit the reviewed draft

Submitting is performed by the server so that the browser does not need to download and re-upload session photos. Repeating a submit request must not create duplicate items.

## State model

```mermaid
stateDiagram-v2
    [*] --> capturing
    capturing --> queued: finish after all uploads
    capturing --> deleted: user deletes
    queued --> analyzing: worker claims session
    analyzing --> ready_for_review: draft persisted
    analyzing --> analysis_failed: provider or validation error
    analysis_failed --> queued: retry
    ready_for_review --> submitting: submit reviewed draft
    submitting --> ready_for_review: recoverable item failure
    submitting --> completed: all items and photos created
    capturing --> expired: retention policy
    analysis_failed --> expired: retention policy
    ready_for_review --> expired: retention policy
    completed --> [*]
    deleted --> [*]
    expired --> [*]
```

| State | User-visible meaning | Mutable operations |
| --- | --- | --- |
| `capturing` | Photos may still be added, removed, or regrouped | Add/delete/regroup photo, change location, finish, delete |
| `queued` | Waiting for server analysis | Delete or cancel before worker claim |
| `analyzing` | AI analysis is running | View progress only |
| `analysis_failed` | Analysis did not produce a draft | Retry, change a missing location, delete |
| `ready_for_review` | Draft is available and autosaved | Edit/correct draft, submit, delete |
| `submitting` | Items and attachments are being created | View progress; retry is server-managed |
| `completed` | All requested items were created | View created item links; delete history record |
| `deleted` / `expired` | Terminal cleanup states | None |

`analyzing` uses a lease timestamp. A session whose worker lease expires after a crash returns to `queued`. A submit operation stores progress per draft item so a crash can resume without creating duplicate items.

## Functional requirements

### Session management

- **CAP-001:** A location is required before a session can be created.
- **CAP-002:** The server validates that the location belongs to the active collection and is a location entity.
- **CAP-003:** Only the creating user may list, open, mutate, analyze, submit, or delete a session.
- **CAP-004:** A user may have at most five non-terminal sessions by default.
- **CAP-005:** Session list results are paginated and ordered by most recent activity.
- **CAP-006:** A session records timestamps for creation, last update, sealing, analysis, completion, and expiration, plus a capture revision that changes when its photo set or grouping changes.
- **CAP-007:** Location may change while `capturing`. After sealing, a deleted or inaccessible location must be replaced before retry or submission.

### Camera and local durability

- **CAP-010:** Supported browsers use `navigator.mediaDevices.getUserMedia` for a persistent camera stream.
- **CAP-011:** The default video constraint requests the environment-facing camera but does not fail when only another camera is available.
- **CAP-012:** Every shutter press stores the photo in IndexedDB before it is considered captured.
- **CAP-013:** The camera remains active while uploads run.
- **CAP-014:** The app prevents duplicate shutter actions while one frame is being encoded.
- **CAP-015:** The UI falls back to the current system camera/file picker when the Media Capture API is unavailable, the page is not a secure context, or permission is denied.
- **CAP-016:** Local queue recovery does not depend on service-worker Background Sync.
- **CAP-017:** Same-item mode is off by default, and its visible toggle state is restored after a same-device reload.
- **CAP-018:** While same-item mode is on, photos share the active `captureGroupId` until **Next Item** or turning the mode off closes that group.
- **CAP-019:** Group membership and active-group state are committed to IndexedDB before the UI reports a grouped photo captured.

### Photo storage and upload

- **CAP-020:** Each photo upload is a separate multipart request.
- **CAP-021:** `clientPhotoId` is unique within a session and acts as an idempotency key.
- **CAP-022:** The server enforces configured MIME, per-file size, session photo-count, and collection authorization limits.
- **CAP-023:** Server detection validates file content; it does not trust the extension or client MIME type.
- **CAP-024:** Photo ordering is stable and independent of upload completion order.
- **CAP-025:** Deleting an uploaded photo during `capturing` deletes both its database record and temporary blob.
- **CAP-026:** Finishing a session requires at least one server-confirmed photo and an exact expected count.
- **CAP-027:** A photo may have one nullable `captureGroupId`. A non-null value means all photos with that ID in the session are explicit views of the same physical item.
- **CAP-028:** Group membership may be changed only while `capturing`, is owner-scoped, and increments the session capture revision.
- **CAP-029:** Finishing validates both `expectedPhotoCount` and `expectedCaptureRevision` before sealing the immutable photo and grouping snapshot.

### Analysis

- **CAP-030:** Finishing queues analysis and responds without waiting for the AI provider.
- **CAP-031:** Analysis runs from server-stored photos and uses the existing `AICaptureService` prompt and sanitization.
- **CAP-032:** Only one worker may analyze a session at a time.
- **CAP-033:** Transient upstream failures retry up to three times with backoff; invalid requests fail without automatic retry.
- **CAP-034:** A successful result and warnings are persisted as the session draft before the state becomes `ready_for_review`.
- **CAP-035:** Draft photo references use stable server photo IDs. Indexes may be derived only at the provider boundary.
- **CAP-036:** The server must never send photos from one session, collection, or user in another session's analysis.
- **CAP-037:** Explicit same-item groups produce at most one initial draft item per `captureGroupId`; all group photos are assigned to it.
- **CAP-038:** The provider contract labels grouped image inputs and returns the stable `captureGroupId` with the item suggestion. The server rejects or repairs output that duplicates a group.
- **CAP-039:** Conflicting evidence inside an explicit group produces a safe warning and `needsReview`; it does not silently split the group into separate inventory items.

### Review and submission

- **CAP-040:** Every draft edit is explicitly saved or autosaved before the UI reports it saved.
- **CAP-041:** Draft updates include an expected revision and reject stale writes with HTTP 409.
- **CAP-042:** AI corrections operate on server-stored photos and the latest persisted draft.
- **CAP-043:** Submission revalidates location, item type IDs, tag IDs, quantities, names, and photo ownership.
- **CAP-044:** Submission creates each inventory item at most once for a given session draft `clientId`.
- **CAP-045:** Session photos are copied into normal item attachments on the server, including normal thumbnail generation and primary-photo selection.
- **CAP-046:** If one item fails, successful item mappings remain recorded and retry processes only unfinished items.
- **CAP-047:** The session becomes `completed` only after every draft item and assigned attachment succeeds.
- **CAP-048:** No item is created during capture, upload, analysis, or correction.
- **CAP-049:** Review initially preserves explicit group photo assignments but permits the user to split, merge, or reassign draft items before submission.

## API design

All routes use the existing authenticated active-collection middleware. Resource reads must apply both collection and owner-user predicates.

| Method | Route | Purpose |
| --- | --- | --- |
| `POST` | `/v1/ai/capture/sessions` | Create a session with `locationId` |
| `GET` | `/v1/ai/capture/sessions` | List the current user's sessions |
| `GET` | `/v1/ai/capture/sessions/{sessionId}` | Get session, progress, photos, and draft |
| `PATCH` | `/v1/ai/capture/sessions/{sessionId}` | Change location when allowed |
| `DELETE` | `/v1/ai/capture/sessions/{sessionId}` | Delete session and temporary photos |
| `POST` | `/v1/ai/capture/sessions/{sessionId}/photos` | Idempotently upload one photo with optional `captureGroupId` |
| `GET` | `/v1/ai/capture/sessions/{sessionId}/photos/{photoId}` | Read an authorized preview or original |
| `DELETE` | `/v1/ai/capture/sessions/{sessionId}/photos/{photoId}` | Delete a photo while capturing |
| `PATCH` | `/v1/ai/capture/sessions/{sessionId}/photos/{photoId}` | Reassign or clear `captureGroupId` while capturing |
| `POST` | `/v1/ai/capture/sessions/{sessionId}/finish` | Seal the expected photo set and queue analysis |
| `POST` | `/v1/ai/capture/sessions/{sessionId}/retry-analysis` | Requeue a failed analysis |
| `PUT` | `/v1/ai/capture/sessions/{sessionId}/draft` | Persist an edited draft using `revision` |
| `POST` | `/v1/ai/capture/sessions/{sessionId}/corrections` | Ask AI to revise the latest draft |
| `POST` | `/v1/ai/capture/sessions/{sessionId}/submit` | Idempotently create inventory items and attachments |

The existing `POST /v1/ai/capture/analyze` route remains available during rollout. The session UI must use only session routes. The direct route can be deprecated after session stability is proven.

Photo upload accepts an optional UUID `captureGroupId` multipart field. The client generates this identifier and persists it before upload, just as it does `clientPhotoId`. Replaying the same `clientPhotoId` returns the original photo and its current group assignment. A replay does not implicitly move an existing photo; group changes use the explicit photo `PATCH` route.

`POST /finish` accepts both `expectedPhotoCount` and `expectedCaptureRevision`. Add, delete, and group-reassignment operations increment `captureRevision`. This prevents a delayed upload or grouping request from racing with sealing.

### Representative session response

```json
{
  "id": "6f76e4de-efea-4e5a-b7e5-feb06862548a",
  "status": "ready_for_review",
  "location": { "id": "...", "name": "Garage" },
  "photoCount": 5,
  "uploadedPhotoCount": 5,
  "captureRevision": 7,
  "photos": [
    {
      "id": "photo-1",
      "position": 0,
      "captureGroupId": "73586682-83c3-43f7-8764-6470e39bd5b5"
    },
    {
      "id": "photo-2",
      "position": 1,
      "captureGroupId": "73586682-83c3-43f7-8764-6470e39bd5b5"
    },
    {
      "id": "photo-3",
      "position": 2,
      "captureGroupId": null
    }
  ],
  "analysisAttempts": 1,
  "draftRevision": 3,
  "draft": {
    "items": [],
    "warnings": []
  },
  "createdItems": [],
  "createdAt": "2026-08-16T12:00:00Z",
  "updatedAt": "2026-08-16T12:02:00Z",
  "expiresAt": "2026-09-15T12:02:00Z"
}
```

Errors expose stable machine-readable codes such as `SESSION_FULL`, `PHOTO_COUNT_MISMATCH`, `CAPTURE_REVISION_MISMATCH`, `INVALID_STATE`, `LOCATION_MISSING`, `ANALYSIS_UPSTREAM_FAILED`, and `DRAFT_REVISION_CONFLICT`. Provider response bodies and credentials are never returned to the browser.

## Data model

### `ai_capture_sessions`

| Field | Notes |
| --- | --- |
| `id` | UUID primary key |
| `group_id` | Active collection owner |
| `user_id` | Session owner |
| `location_id` | Selected location |
| `location_name_snapshot` | Display fallback if location later disappears |
| `status` | State-machine enum |
| `draft_json` | Sanitized current `AICaptureDraft`, nullable |
| `draft_revision` | Incremented on every accepted draft mutation |
| `photo_count` | Expected sealed photo count |
| `capture_revision` | Incremented whenever a photo is added, deleted, or reassigned to a group |
| `analysis_attempts` | Worker retry count |
| `worker_lease_until` | Crash recovery for claimed work |
| `error_code` / `error_message` | User-safe last failure, nullable |
| `created_at` / `updated_at` | Standard timestamps |
| `finished_at` / `analyzed_at` / `completed_at` | Lifecycle timestamps, nullable |
| `expires_at` | Temporary-data retention deadline |

Indexes cover `(user_id, updated_at)`, `(group_id, user_id, status)`, and `(status, worker_lease_until)`.

### `ai_capture_photos`

| Field | Notes |
| --- | --- |
| `id` | UUID primary key |
| `session_id` | Owning session |
| `client_photo_id` | Client idempotency key |
| `position` | Stable capture order |
| `capture_group_id` | Nullable client-generated UUID identifying explicit views of the same item |
| `original_name` | Sanitized display name |
| `path` | Temporary blob path |
| `mime_type` | Server-detected type |
| `size_bytes` | Stored byte count |
| `content_hash` | Integrity and diagnostic hash; not global deduplication |
| `created_at` | Upload completion time |

Unique constraints cover `(session_id, client_photo_id)` and `(session_id, position)`.

No separate group table is required for the first version. A group exists when one or more photos in a session share a non-null `capture_group_id`; its display order is the minimum global photo position in that group. Empty groups have no server representation. A later feature requiring group names or independent group lifecycle may normalize groups into their own table.

### Group-aware draft fields

`AICaptureItem` adds an optional `captureGroupId`. It is set only for an item initially derived from an explicit same-item group. `photoIds` remains the authoritative attachment assignment.

| Field | Notes |
| --- | --- |
| `clientId` | Stable draft and submission idempotency key |
| `captureGroupId` | Nullable source group; exactly one initial draft item may reference each explicit group |
| `photoIds` | Stable server photo IDs assigned to the draft item |
| `needsReview` / `reviewReason` | Used when views conflict or grouping appears incorrect |

Splitting or merging cards during review may clear `captureGroupId` from the affected draft items because the user's reviewed structure supersedes the initial capture hint. The sealed photos retain their original `capture_group_id` until temporary cleanup.

### `ai_capture_session_items`

This table records durable submission progress:

| Field | Notes |
| --- | --- |
| `session_id` | Owning session |
| `client_id` | Draft item's stable client ID |
| `entity_id` | Created HomeBox item ID, nullable until created |
| `status` | `pending`, `creating`, `attaching`, `completed`, or `failed` |
| `uploaded_photo_ids` | Persisted attachment progress or normalized child rows |
| `error_code` | Last retryable failure, nullable |

The unique `(session_id, client_id)` constraint provides item-creation idempotency.

Capture photos should not be represented as ordinary `attachments` before item creation. Normal attachments are entity-owned and their deletion semantics do not fit temporary, potentially multi-item capture sources. At submission, the service copies a capture photo to each assigned item's attachment storage. After all copies succeed, the temporary source can be deleted.

## Backend processing

The API process owns a small capture-session worker:

1. Poll for `queued` sessions.
2. Claim one with an atomic state transition and lease.
3. Read the session's photos in position order.
4. Partition explicit same-item groups from ungrouped photos.
5. Analyze each explicit group as one item and analyze the remaining ungrouped set with the existing free-detection behavior.
6. Convert stable photo IDs to provider indexes at each provider boundary.
7. Translate provider indexes back to stable photo IDs, merge results in first-photo order, and enforce one initial item per explicit group.
8. Persist the sanitized draft and mark the session ready.
9. Retry transient failures or persist a safe terminal error.

On process startup and periodically thereafter, expired `analyzing` leases are returned to `queued`. Worker concurrency defaults to one per process so a local model is not unexpectedly saturated. Existing AI timeouts still apply to each attempt.

### Group-aware AI contract

This feature does not change the configured model, OpenAI-compatible API defaults, or model weights. It changes how HomeBox structures the vision request and validates the response.

For an explicit group, the provider instruction states that all supplied images are different views of one physical inventory item and requests exactly one item result. The instruction includes the opaque `captureGroupId`, and the structured response schema requires that same ID. The model should reconcile evidence across views—for example, using one image for the product shape and another for a legible brand or model number—without treating the number of photographs as quantity.

The first implementation should make one provider call per explicit same-item group, processed serially by the session worker. This is intentionally simple and is expected to be more reliable for the configured local Qwen model than asking one prompt to maintain many cross-image identity constraints. All ungrouped photos, if any, use one additional call through the existing multi-item detector. The session-level photo and item limits still apply after results are merged.

Sanitization enforces the contract independently of the model:

- A grouped result must return the requested `captureGroupId`.
- At most one initial item may reference a given explicit group.
- That item receives every available photo ID in its group by default.
- Unknown group IDs, photo IDs, entity type IDs, and tag IDs are removed or rejected using the existing validation policy.
- If the provider returns several items for one group, HomeBox retries the group once with a stricter correction instruction. If the response is still invalid, the complete session enters `analysis_failed`; HomeBox does not guess which duplicate to keep or expose duplicate group items for review.
- If the provider returns one item but warns that the views conflict, HomeBox preserves the single item, assigns all group photos, and marks it `needsReview` with a safe explanation.
- If a group call fails, the session does not silently omit it. The normal analysis retry or failure state applies to the complete session.

AI correction requests retain group annotations. Once the user explicitly splits or merges draft cards, the persisted reviewed draft takes precedence and a correction must not recreate the original grouping unless the user's instruction asks for it.

## Browser and deployment requirements

A continuous in-page camera depends on `getUserMedia`, which mobile browsers expose only in a secure context. Production and LAN deployments that want this experience must serve HomeBox over HTTPS with a certificate trusted by the phone. Plain HTTP LAN addresses should expect the system-camera fallback, which may leave the HomeBox view between photos.

The feature must also handle:

- Camera permission denied or revoked
- No rear-facing camera
- Safari page suspension
- Android browser process eviction
- Device rotation and safe-area insets
- Low-memory canvas encoding failures
- IndexedDB quota exhaustion
- Temporary network loss
- An expired login during background upload

When authentication expires, the local queue remains intact and resumes after login. The UI must not repeatedly upload while receiving authorization failures.

## Security and privacy

- Session routes require normal authentication and active-collection authorization.
- Owner checks are mandatory even for users who share the same collection.
- `captureGroupId` values are opaque UUIDs. The server scopes them to their owning session and never accepts a group reference as authorization to read or mutate photos.
- Temporary photo URLs are authenticated API routes, not public blob URLs.
- Filenames are sanitized and never used as storage paths.
- Blob keys use server-generated identifiers.
- Image type is detected from file bytes and constrained to supported formats.
- Browser normalization strips EXIF metadata, including location metadata, from new session photos.
- AI provider credentials remain server-side.
- Logs include session and request IDs but not image bytes, prompts containing user corrections, provider bodies, or draft descriptions.
- Deleting a session removes database rows and attempts immediate blob deletion. Failed cleanup is retried.
- Completed sessions delete temporary source blobs after all item attachments are confirmed.
- Unfinished and review-ready sessions expire 30 days after last activity. The UI displays the expiration date and refreshes it on meaningful user edits, not passive reads.

## Accessibility

- Shutter, finish, camera switch, torch, close, delete, and retry controls have accessible names.
- The same-item toggle exposes its label and checked state programmatically. **Next Item** includes the active group number in its accessible description and is disabled until that group contains a photo.
- Status is communicated by text and icons, never color alone.
- Shutter confirmation has a non-visual live-region announcement that does not interrupt rapid capture. In same-item mode, it announces the item-group number and number of views.
- Group labels remain understandable without relying on color, and photo reassignment is keyboard and screen-reader operable.
- Motion and flash effects honor reduced-motion preferences.
- The camera tray, finishing screen, session list, and review form are fully keyboard operable.
- Focus returns predictably after closing the photo tray or an error dialog.
- Controls respect mobile safe areas and have at least 44-by-44 CSS-pixel touch targets.

## Reliability and performance targets

- Shutter-to-confirmation: under 300 ms on a representative mid-range phone, excluding exceptional encoding delays.
- The camera preview must not wait for network requests.
- Upload concurrency: two per session by default.
- Capture recovery after reload: no server-confirmed or IndexedDB-committed photo is lost.
- Capture recovery after reload preserves every committed `captureGroupId`, the current toggle state, and whether the next photo starts a new group.
- Sealing uses an exact capture revision so a concurrent photo deletion or grouping edit cannot be omitted.
- Session creation, photo upload, finish, draft update, correction, and submit operations are idempotent where retries can occur.
- Group analysis is serial by default. Progress identifies the current group or ungrouped batch, and each provider call uses the configured AI timeout independently.
- Session-list status polling backs off when the page is hidden. WebSocket events may be added later but are not required for the first release.
- Temporary file cleanup is observable and retryable.

## Observability

Record structured events or metrics without photo or draft contents:

- Sessions created, finished, analyzed, failed, submitted, deleted, and expired
- Photos captured locally, uploaded, retried, failed, and deleted
- Same-item mode enabled or disabled, groups started and closed, and photos reassigned or ungrouped
- Group size at sealing, measured as photos per explicit group, without photo contents or item metadata
- Analysis queue time and provider duration, including per-group calls and the ungrouped batch
- Group-contract retry, conflicting-view warning, invalid group ID, and duplicate-group-result counts
- Draft review time and correction count
- Submission retry and partial-failure counts
- Cleanup failures and temporary storage usage
- Camera fallback reason: insecure context, unsupported API, or permission denial

Logs must include `session_id`, `user_id`, `group_id`, state transition, attempt count, and safe error code where relevant.

## Acceptance criteria

1. On a supported phone served over trusted HTTPS, a user can take the configured maximum number of photos with one uninterrupted camera stream.
2. After each shutter press, the count updates and the next photo can be taken without waiting for upload.
3. Reloading the capture page restores locally committed, not-yet-uploaded photos on the same device.
4. A transient failed upload can retry without creating a duplicate server photo.
5. Finish cannot silently exclude an unuploaded photo or a committed grouping change.
6. After finish succeeds, the user can close the browser and later find the session in `ready_for_review` or a clearly recoverable failed state.
7. Opening a ready session restores its photos, AI warnings, current draft, and edits.
8. Two tabs editing the same draft cannot silently overwrite one another.
9. Retrying analysis or submission does not duplicate drafts, items, or attachments.
10. Another user in the same collection cannot access the session or its photos.
11. Plain HTTP or denied camera permission presents a working system-camera/upload fallback with a clear explanation.
12. Completing submission creates items in the selected location with assigned photos, then removes temporary capture blobs.
13. No inventory items exist before explicit submission.
14. AI output remains restricted to the currently documented visual metadata fields.
15. With same-item mode off, capture and free-detection behavior are unchanged.
16. Three photos taken before **Next Item** share one stable group after reload, upload, and sealing.
17. Tapping **Next Item** keeps the camera open and causes the next photo to start a distinct group.
18. Each explicit group produces at most one initial draft item, and that item starts with every photo from the group assigned.
19. Invalid duplicate results or unknown group references never create duplicate or omitted items silently; the session retries or exposes a recoverable analysis failure.
20. A conflicting but structurally valid grouped result appears as one `needsReview` item with a clear warning.
21. During capture, a user can reassign or ungroup a photo without re-uploading it. During review, a user can split, merge, and reassign suggestions before submission.
22. Enabling same-item mode works with the existing configured OpenAI-compatible model and API defaults; it requires no model retraining or configuration change.

## Test plan

### Backend

- Repository tests for ownership filters, state transitions, leases, expiry, and cleanup
- Handler tests for every route, invalid state, collection mismatch, owner mismatch, group reassignment, and idempotency
- Upload tests for content detection, size limits, duplicate `clientPhotoId`, group-ID persistence, ordering, and blob cleanup
- Capture-revision tests for add, delete, and regroup operations, including a stale finish request
- Worker tests for success, transient retry, permanent failure, expired lease recovery, location deletion, grouped calls, and mixed grouped/ungrouped sessions
- AI contract tests for unknown group IDs, missing photo IDs, duplicate results for one group, conflicting-view warnings, and a failed group call that must fail the complete session
- Submission tests for partial attachment failure, process restart, duplicate request, and multi-item photo copying
- Concurrency tests for double finish, regroup-versus-finish, double worker claim, stale draft revision, and double submit

### Frontend

- Camera component tests with mocked media streams and track cleanup
- Same-item toggle and **Next Item** tests, including off-by-default behavior, group numbering, mode transitions, and photo-limit behavior
- IndexedDB queue tests for reload recovery, group and active-mode persistence, retry, deletion, and quota failure
- Photo-tray tests for group labels, reassignment, ungrouping, deleting an empty group, and returning to the live stream
- API tests for idempotency fields and state responses
- Session list tests for every status and primary action
- Review autosave tests for debounce, failed save, and revision conflict
- Accessibility tests for camera controls, toggle state, group labels, live-region group announcements, focus, and keyboard reassignment

### End-to-end device matrix

- Current iOS Safari
- Current Android Chrome
- Desktop Chrome, Firefox, and Safari with webcam or fallback
- Trusted HTTPS deployment
- Plain HTTP LAN deployment fallback
- Slow network, offline/reconnect, expired login, page reload, and browser restart
- Multiple views across reload and offline recovery, including **Next Item**, toggle-off/on boundaries, and photo reassignment
- Mixed sessions containing multiple explicit groups and ungrouped room-level photos
- System-camera and upload fallbacks with grouping retained between photo selections
- AI provider timeout and malformed AI response
- Local Qwen evaluation covering multiple angles of one item, similar-looking separate item groups, conflicting views, and duplicate-result suppression
- Server restart during analysis and submission

## Rollout plan

### Phase 1: durable sessions and storage

- Add schemas, repositories, ownership rules, session CRUD, single-photo upload, and cleanup.
- Keep the current photo picker as the capture UI.
- Prove persistence and idempotency before adding the camera stream.

### Phase 2: asynchronous analysis and session review

- Add finish, worker leases, persisted drafts, session list, autosaved review, correction, and server-side submission.
- Keep the direct analysis route available as a fallback.

### Phase 3: continuous camera

- Add the `getUserMedia` camera, IndexedDB upload queue, photo tray, permission UX, and system-camera fallback.
- Validate on real iOS and Android devices over HTTPS.

### Phase 4: hardening

- Add expiry UI, cleanup retries, storage metrics, concurrency tests, and recovery testing.
- Deprecate the direct in-memory analysis flow after session telemetry and support feedback show acceptable stability.

### Phase 5: multiple views of the same item

- Add nullable photo group IDs, capture revisions, upload grouping, and the capture-only regrouping route.
- Add the camera toggle, **Next Item**, grouped tray, local queue recovery, and accessible group feedback.
- Add serial group-aware analysis, strict response sanitization, mixed grouped/ungrouped result merging, and review warnings.
- Evaluate the prompt and schema against the configured local Qwen model before enabling the toggle by default in production builds. No model retraining or configuration change is required.

## Future extensions

- Larger sessions split into explicit AI batches with cross-batch item deduplication
- Named capture groups, group notes, or room sweeps beyond the same-item relationship
- Optional shared sessions for collection collaborators
- Notifications when a long-running session becomes ready for review
- On-device quality checks for blur, darkness, or duplicate frames
- Barcode hints captured alongside photos
- WebSocket session progress updates
