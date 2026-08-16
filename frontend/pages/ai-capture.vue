<script setup lang="ts">
  import { toast } from "@/components/ui/sonner";
  import { useI18n } from "vue-i18n";
  import BaseContainer from "~/components/Base/Container.vue";
  import AICaptureCamera from "~/components/AI/CaptureCamera.vue";
  import LocationCapturePicker from "~/components/Location/CapturePicker.vue";
  import TagSelector from "~/components/Tag/Selector.vue";
  import { Button } from "~/components/ui/button";
  import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "~/components/ui/card";
  import { Checkbox } from "~/components/ui/checkbox";
  import { Input } from "~/components/ui/input";
  import { Label } from "~/components/ui/label";
  import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "~/components/ui/select";
  import { Textarea } from "~/components/ui/textarea";
  import type {
    AICaptureDraft,
    AICaptureItem,
    AICaptureMoveDisposition,
    AICaptureReanalysis,
    AICaptureSession,
    AICaptureSessionPhoto,
  } from "~/lib/api/classes/ai-capture";
  import type { APISummary } from "~/lib/api/types/data-contracts";
  import {
    clearCaptureQueueSession,
    createCaptureID,
    deleteCaptureQueuePhoto,
    getCaptureQueueSessionState,
    listCaptureQueuePhotos,
    normalizeCaptureImage,
    putCaptureQueuePhoto,
    putCaptureQueuePhotoWithState,
    putCaptureQueueSessionState,
    type CaptureQueuePhoto,
    type CaptureQueueSessionState,
  } from "~/lib/ai-capture-queue";
  import MdiAlertCircleOutline from "~icons/mdi/alert-circle-outline";
  import MdiArrowLeft from "~icons/mdi/arrow-left";
  import MdiCamera from "~icons/mdi/camera";
  import MdiCheckCircle from "~icons/mdi/check-circle";
  import MdiClockOutline from "~icons/mdi/clock-outline";
  import MdiDelete from "~icons/mdi/delete";
  import MdiImageMultiple from "~icons/mdi/image-multiple";
  import MdiLoading from "~icons/mdi/loading";
  import MdiMagicStaff from "~icons/mdi/magic-staff";
  import MdiMapMarker from "~icons/mdi/map-marker";
  import MdiPackageVariant from "~icons/mdi/package-variant";
  import MdiPlus from "~icons/mdi/plus";
  import MdiRefresh from "~icons/mdi/refresh";
  import MdiUpload from "~icons/mdi/upload";

  definePageMeta({ middleware: ["auth"] });

  const { t } = useI18n();
  useHead({ title: `HomeBox | ${t("ai_capture.title")}` });

  type CaptureView = "sessions" | "location" | "camera" | "processing" | "review" | "done";
  type CaptureLandingTab = "review" | "activity";
  type CaptureLocation = { id: string; name: string };
  type PendingReviewEntry = {
    key: string;
    session: AICaptureSession;
    item: AICaptureItem;
    itemIndex: number;
  };
  type ReanalysisFieldKey =
    "name" | "quantity" | "entityTypeId" | "manufacturer" | "modelNumber" | "description" | "tagIds";

  const reanalysisFieldKeys: ReanalysisFieldKey[] = [
    "name",
    "quantity",
    "entityTypeId",
    "manufacturer",
    "modelNumber",
    "description",
    "tagIds",
  ];
  const moveDispositionOptions: AICaptureMoveDisposition[] = [
    "undecided",
    "keep",
    "sell",
    "give_away",
    "donate",
    "recycle",
    "trash",
  ];

  const api = useUserApi();
  const publicApi = usePublicApi();
  const entityTypeStore = useEntityTypeStore();
  const tagStore = useTagStore();
  const locationStore = useLocationStore();

  const view = ref<CaptureView>("sessions");
  const landingTab = ref<CaptureLandingTab>("review");
  const status = ref<APISummary | null>(null);
  const sessions = ref<AICaptureSession[]>([]);
  const activeSession = ref<AICaptureSession | null>(null);
  const selectedLocation = ref<CaptureLocation | null>(null);
  const queue = ref<CaptureQueuePhoto[]>([]);
  const previewURLs = ref<Record<string, string>>({});
  const correction = ref("");
  const reanalysisInstruction = ref("");
  const reanalysisProvider = ref("default");
  const bulkMoveDisposition = ref<AICaptureMoveDisposition>("undecided");
  const selectedReviewItemIds = ref<string[]>([]);
  const selectedPendingItemKeys = ref<string[]>([]);
  const pendingBulkMoveDisposition = ref<AICaptureMoveDisposition>("undecided");
  const reanalyzingItemIds = ref<string[]>([]);
  const reanalysisSuggestions = ref<Record<string, AICaptureReanalysis>>({});
  const reanalysisErrors = ref<Record<string, string>>({});
  const undoDraft = ref<AICaptureDraft | null>(null);
  const loading = ref(false);
  const saving = ref(false);
  const submitting = ref(false);
  const rollingOver = ref(false);
  const nextPosition = ref(0);
  const sameItemMode = ref(false);
  const activeCaptureGroupId = ref<string>();
  const regroupingPhotoIds = ref<string[]>([]);
  const inFlight = new Map<string, Promise<void>>();
  let captureChain = Promise.resolve();
  let pollTimer: ReturnType<typeof setInterval> | undefined;
  let autosaveTimer: ReturnType<typeof setTimeout> | undefined;
  let reanalysisDismissChain = Promise.resolve();

  const itemTypes = computed(() => entityTypeStore.itemTypes);
  const tags = computed(() => tagStore.tags);
  const maxProviderPhotos = computed(() => status.value?.ai?.maxPhotos || 8);
  const maxSessionPhotos = computed(() => status.value?.ai?.maxSessionPhotos || maxProviderPhotos.value || 24);
  const aiEnabled = computed(() => status.value?.ai?.enabled !== false);
  const failedUploads = computed(() => queue.value.filter(photo => photo.status === "failed"));
  const queuedUploads = computed(() => queue.value.filter(photo => photo.status !== "failed").length);
  const capturedCount = computed(() => (activeSession.value?.photos.length || 0) + queue.value.length);
  const draft = computed(() => activeSession.value?.draft);
  const completedReviewItemIDs = computed(
    () =>
      new Set(
        (activeSession.value?.submissions || [])
          .filter(submission => submission.status === "completed")
          .map(submission => submission.clientId)
      )
  );
  const reviewItems = computed(() =>
    (draft.value?.items || [])
      .map((item, itemIndex) => ({ item, itemIndex }))
      .filter(entry => !completedReviewItemIDs.value.has(entry.item.clientId))
  );
  const pendingReviewItems = computed<PendingReviewEntry[]>(() =>
    sessions.value.flatMap(session => {
      if (session.status !== "ready_for_review" || !session.draft) return [];
      const completed = new Set(
        (session.submissions || [])
          .filter(submission => submission.status === "completed")
          .map(submission => submission.clientId)
      );
      return session.draft.items
        .map((item, itemIndex) => ({ key: `${session.id}:${item.clientId}`, session, item, itemIndex }))
        .filter(entry => !completed.has(entry.item.clientId));
    })
  );
  const selectedPendingItems = computed(() => {
    const selected = new Set(selectedPendingItemKeys.value);
    return pendingReviewItems.value.filter(entry => selected.has(entry.key));
  });
  const allPendingItemsSelected = computed(
    () => pendingReviewItems.value.length > 0 && selectedPendingItems.value.length === pendingReviewItems.value.length
  );
  const activitySessions = computed(() =>
    sessions.value.filter(
      session =>
        session.status !== "ready_for_review" ||
        !session.draft?.items.some(
          item =>
            !(session.submissions || []).some(
              submission => submission.clientId === item.clientId && submission.status === "completed"
            )
        )
    )
  );
  const sessionPhotos = computed(() => activeSession.value?.photos || []);
  const activeStatus = computed(() => activeSession.value?.status || "capturing");
  const reanalysisActive = computed(() =>
    ["queued", "processing", "waiting"].includes(activeSession.value?.reanalysis?.status || "")
  );
  const reanalysisProviders = computed(() => {
    const configured = status.value?.ai?.providers?.filter(provider => provider.enabled) || [];
    return configured.length
      ? configured
      : [
          {
            id: "default",
            name: "Qwen",
            model: status.value?.ai?.model || "",
            enabled: true,
          },
        ];
  });
  const allReviewItemsSelected = computed(
    () =>
      !!reviewItems.value.length &&
      reviewItems.value.every(({ item }) => selectedReviewItemIds.value.includes(item.clientId))
  );
  const captureGroups = computed(() => {
    const firstPositions = new Map<string, number>();
    const photos = [
      ...(activeSession.value?.photos || []).map(photo => ({
        position: photo.position,
        captureGroupId: photo.captureGroupId,
      })),
      ...queue.value.map(photo => ({
        position: photo.position,
        captureGroupId: photo.captureGroupId,
      })),
    ];
    for (const photo of photos) {
      if (!photo.captureGroupId) continue;
      const existing = firstPositions.get(photo.captureGroupId);
      if (existing === undefined || photo.position < existing) firstPositions.set(photo.captureGroupId, photo.position);
    }
    return [...firstPositions.entries()]
      .sort((left, right) => left[1] - right[1])
      .map(([id], index) => ({ id, number: index + 1 }));
  });
  const activeGroupNumber = computed(
    () => captureGroups.value.find(group => group.id === activeCaptureGroupId.value)?.number
  );
  const activeGroupViewCount = computed(() => {
    if (!activeCaptureGroupId.value) return 0;
    return (
      (activeSession.value?.photos.filter(photo => photo.captureGroupId === activeCaptureGroupId.value).length || 0) +
      queue.value.filter(photo => photo.captureGroupId === activeCaptureGroupId.value).length
    );
  });

  function captureSessionState(sessionId: string): CaptureQueueSessionState {
    return {
      sessionId,
      sameItemMode: sameItemMode.value,
      activeCaptureGroupId: activeCaptureGroupId.value,
      updatedAt: Date.now(),
    };
  }

  async function persistCaptureState() {
    if (!activeSession.value) return;
    await putCaptureQueueSessionState(captureSessionState(activeSession.value.id));
  }

  function groupLabel(groupId?: string | null) {
    if (!groupId) return t("ai_capture.camera.ungrouped");
    const number = captureGroups.value.find(group => group.id === groupId)?.number;
    return number ? t("ai_capture.camera.item_group", { group: number }) : t("ai_capture.camera.item_group_unknown");
  }

  function responseMessage(response: { data?: unknown }) {
    const data = response.data as { error?: string; message?: string } | undefined;
    return data?.error || data?.message || t("ai_capture.errors.request_failed");
  }

  function statusLabel(sessionStatus: AICaptureSession["status"]) {
    return t(`ai_capture.status.${sessionStatus}`);
  }

  function statusClass(sessionStatus: AICaptureSession["status"]) {
    if (sessionStatus === "analysis_failed") return "bg-destructive/10 text-destructive";
    if (sessionStatus === "ready_for_review") return "bg-amber-500/15 text-amber-700 dark:text-amber-300";
    if (sessionStatus === "completed") return "bg-green-500/15 text-green-700 dark:text-green-300";
    return "bg-muted text-muted-foreground";
  }

  function sessionTime(value: string) {
    return new Intl.DateTimeFormat(undefined, {
      dateStyle: "medium",
      timeStyle: "short",
    }).format(new Date(value));
  }

  function storeSession(session: AICaptureSession) {
    const index = sessions.value.findIndex(candidate => candidate.id === session.id);
    if (index >= 0) sessions.value[index] = session;
    else sessions.value.unshift(session);
  }

  function replaceSession(session: AICaptureSession) {
    activeSession.value = session;
    syncReanalysis(session);
    selectedLocation.value = session.location || null;
    storeSession(session);
  }

  function syncReanalysis(session: AICaptureSession) {
    reanalysisSuggestions.value = { ...(session.reanalysis?.suggestions || {}) };
    reanalysisErrors.value = { ...(session.reanalysis?.errors || {}) };
    reanalyzingItemIds.value = reanalysisActiveStatus(session.reanalysis?.status)
      ? [...(session.reanalysis?.pendingClientIds || [])]
      : [];
  }

  function reanalysisActiveStatus(value?: string) {
    return value === "queued" || value === "processing" || value === "waiting";
  }

  function providerName(providerId: string) {
    return reanalysisProviders.value.find(provider => provider.id === providerId)?.name || providerId;
  }

  function toggleReviewItem(clientId: string, checked: boolean) {
    selectedReviewItemIds.value = checked
      ? [...new Set([...selectedReviewItemIds.value, clientId])]
      : selectedReviewItemIds.value.filter(id => id !== clientId);
  }

  function toggleAllReviewItems(checked: boolean) {
    selectedReviewItemIds.value = checked ? reviewItems.value.map(({ item }) => item.clientId) : [];
  }

  function moveDispositionLabel(value: AICaptureMoveDisposition) {
    return t(`ai_capture.review.move_disposition_${value}`);
  }

  function moveDispositionNoteLabel(value: AICaptureMoveDisposition) {
    return t(`ai_capture.review.move_note_${value}`);
  }

  function moveDispositionNotePlaceholder(value: AICaptureMoveDisposition) {
    return t(`ai_capture.review.move_note_${value}_placeholder`);
  }

  function applyBulkMoveDisposition() {
    if (!activeSession.value?.draft || selectedReviewItemIds.value.length === 0) return;
    const selected = new Set(selectedReviewItemIds.value);
    for (const item of activeSession.value.draft.items) {
      if (selected.has(item.clientId)) item.moveDisposition = bulkMoveDisposition.value;
    }
    toast.success(
      t("ai_capture.review.move_bulk_applied", {
        count: selectedReviewItemIds.value.length,
        disposition: moveDispositionLabel(bulkMoveDisposition.value),
      })
    );
  }

  function resetReviewReanalysis() {
    reanalysisInstruction.value = "";
    reanalysisProvider.value = "default";
    selectedReviewItemIds.value = [];
    reanalyzingItemIds.value = [];
    reanalysisSuggestions.value = {};
    reanalysisErrors.value = {};
    undoDraft.value = null;
  }

  async function loadSessions() {
    const response = await api.aiCapture.listSessions();
    if (!response.error) sessions.value = response.data.items || [];
  }

  function pendingItemPhotos(entry: PendingReviewEntry) {
    const selected = new Set(entry.item.photoIds || []);
    return entry.session.photos.filter(photo => selected.has(photo.id));
  }

  function togglePendingItem(key: string, checked: boolean) {
    selectedPendingItemKeys.value = checked
      ? [...new Set([...selectedPendingItemKeys.value, key])]
      : selectedPendingItemKeys.value.filter(value => value !== key);
  }

  function toggleAllPendingItems(checked: boolean) {
    selectedPendingItemKeys.value = checked ? pendingReviewItems.value.map(entry => entry.key) : [];
  }

  function groupPendingItemsBySession(entries: PendingReviewEntry[]) {
    const grouped = new Map<string, PendingReviewEntry[]>();
    for (const entry of entries) {
      const existing = grouped.get(entry.session.id) || [];
      existing.push(entry);
      grouped.set(entry.session.id, existing);
    }
    return grouped;
  }

  async function openPendingItem(entry: PendingReviewEntry) {
    await openSession(entry.session);
    if (view.value !== "review") return;
    await nextTick();
    document.getElementById(`review-item-${entry.item.clientId}`)?.scrollIntoView({ behavior: "smooth" });
  }

  async function applyPendingBulkMoveDisposition() {
    if (selectedPendingItems.value.length === 0 || saving.value) return;
    const bySession = groupPendingItemsBySession(selectedPendingItems.value);
    saving.value = true;
    let updated = 0;
    try {
      for (const entries of bySession.values()) {
        const session = entries[0]!.session;
        if (!session.draft) continue;
        const draft = cloneDraft(session.draft);
        const selected = new Set(entries.map(entry => entry.item.clientId));
        for (const item of draft.items) {
          if (selected.has(item.clientId)) item.moveDisposition = pendingBulkMoveDisposition.value;
        }
        const response = await api.aiCapture.saveDraft(session.id, session.draftRevision, draft);
        if (response.error) throw new Error(responseMessage(response));
        storeSession(response.data);
        updated += entries.length;
      }
      toast.success(
        t("ai_capture.review.move_bulk_applied", {
          count: updated,
          disposition: moveDispositionLabel(pendingBulkMoveDisposition.value),
        })
      );
    } catch (error) {
      console.error(error);
      toast.error(t("ai_capture.errors.save_draft"));
    } finally {
      saving.value = false;
    }
  }

  async function submitPendingItems(entries: PendingReviewEntry[]) {
    if (entries.length === 0 || submitting.value) return;
    if (entries.some(entry => !entry.item.name.trim() || !entry.item.entityTypeId || entry.item.quantity <= 0)) {
      toast.error(t("ai_capture.errors.review_required"));
      return;
    }
    const bySession = groupPendingItemsBySession(entries);
    submitting.value = true;
    let created = 0;
    let failed = 0;
    try {
      for (const sessionEntries of bySession.values()) {
        const session = sessionEntries[0]!.session;
        const clientIds = sessionEntries.map(entry => entry.item.clientId);
        const response = await api.aiCapture.submitItems(session.id, session.draftRevision, clientIds);
        if (response.error || response.data.errorCode) {
          failed += sessionEntries.length;
          if (!response.error) storeSession(response.data);
          continue;
        }
        storeSession(response.data);
        created += sessionEntries.length;
        if (response.data.status === "completed") await clearCaptureQueueSession(response.data.id);
      }
      await locationStore.refreshChildren();
      await loadSessions();
      const remainingKeys = new Set(pendingReviewItems.value.map(entry => entry.key));
      selectedPendingItemKeys.value = selectedPendingItemKeys.value.filter(key => remainingKeys.has(key));
    } catch (error) {
      console.error(error);
      failed += Math.max(0, entries.length - created - failed);
    } finally {
      submitting.value = false;
    }
    if (created) toast.success(t("ai_capture.review.items_created", { count: created }));
    if (failed) toast.error(t("ai_capture.errors.partial_submit"));
  }

  function revokePreviews() {
    for (const url of Object.values(previewURLs.value)) URL.revokeObjectURL(url);
    previewURLs.value = {};
  }

  async function restoreQueue(sessionId: string) {
    revokePreviews();
    queue.value = await listCaptureQueuePhotos(sessionId);
    for (const photo of queue.value) {
      if (photo.captureGroupId === undefined) {
        photo.captureGroupId = null;
        await putCaptureQueuePhoto(photo);
      }
    }
    const savedState = await getCaptureQueueSessionState(sessionId);
    sameItemMode.value = savedState?.sameItemMode || false;
    activeCaptureGroupId.value = savedState?.activeCaptureGroupId;
    const urls: Record<string, string> = {};
    for (const photo of queue.value) urls[photo.clientPhotoId] = URL.createObjectURL(photo.blob);
    previewURLs.value = urls;
    const positions = [
      ...queue.value.map(photo => photo.position),
      ...(activeSession.value?.photos.map(photo => photo.position) || []),
    ];
    nextPosition.value = positions.length ? Math.max(...positions) + 1 : 0;
    for (const photo of queue.value) {
      if (photo.status === "uploading") {
        photo.status = "pending";
        await putCaptureQueuePhoto(photo);
      }
    }
    if (activeCaptureGroupId.value && activeGroupViewCount.value === 0) {
      activeCaptureGroupId.value = undefined;
      await persistCaptureState();
    }
    pumpUploads();
  }

  async function openSession(session: AICaptureSession) {
    if (activeSession.value?.id !== session.id) resetReviewReanalysis();
    loading.value = true;
    try {
      const response = await api.aiCapture.getSession(session.id);
      if (response.error) throw new Error(responseMessage(response));
      replaceSession(response.data);
      if (response.data.status === "capturing") {
        await restoreQueue(response.data.id);
        view.value = "camera";
      } else if (response.data.status === "ready_for_review") {
        view.value = "review";
      } else if (response.data.status === "completed") {
        view.value = "done";
      } else {
        view.value = "processing";
      }
    } catch (error) {
      console.error(error);
      toast.error(t("ai_capture.errors.load_session"));
    } finally {
      loading.value = false;
    }
  }

  function newSession() {
    resetReviewReanalysis();
    activeSession.value = null;
    selectedLocation.value = null;
    queue.value = [];
    sameItemMode.value = false;
    activeCaptureGroupId.value = undefined;
    revokePreviews();
    view.value = "location";
  }

  async function startCapturing() {
    if (!selectedLocation.value || !aiEnabled.value) return;
    loading.value = true;
    try {
      const response = activeSession.value
        ? await api.aiCapture.updateSessionLocation(activeSession.value.id, selectedLocation.value.id)
        : await api.aiCapture.createSession(selectedLocation.value.id);
      if (response.error) throw new Error(responseMessage(response));
      replaceSession(response.data);
      await restoreQueue(response.data.id);
      view.value = "camera";
    } catch (error) {
      console.error(error);
      toast.error(t("ai_capture.errors.create_session"));
    } finally {
      loading.value = false;
    }
  }

  async function storeCapture(file: File) {
    if (!activeSession.value) return;
    if (activeCaptureGroupId.value && activeGroupViewCount.value >= maxProviderPhotos.value) {
      toast.error(t("ai_capture.errors.item_group_full", { count: maxProviderPhotos.value }));
      return;
    }
    if (capturedCount.value >= maxSessionPhotos.value) {
      if (activeCaptureGroupId.value) {
        toast.error(t("ai_capture.errors.item_group_full", { count: maxProviderPhotos.value }));
        return;
      }
      if (!(await rolloverCaptureBatch())) return;
    }
    if (!activeSession.value) {
      return;
    }
    const position = nextPosition.value++;
    try {
      const normalized = await normalizeCaptureImage(file, file.name);
      const clientPhotoId = createCaptureID();
      const captureGroupId = sameItemMode.value ? activeCaptureGroupId.value || createCaptureID() : null;
      const photo: CaptureQueuePhoto = {
        key: `${activeSession.value.id}:${clientPhotoId}`,
        sessionId: activeSession.value.id,
        clientPhotoId,
        position,
        captureGroupId,
        name: normalized.name,
        blob: normalized,
        status: "pending",
        attempts: 0,
        createdAt: Date.now(),
      };
      const nextState: CaptureQueueSessionState = {
        sessionId: activeSession.value.id,
        sameItemMode: sameItemMode.value,
        activeCaptureGroupId: captureGroupId || undefined,
        updatedAt: Date.now(),
      };
      await putCaptureQueuePhotoWithState(photo, nextState);
      activeCaptureGroupId.value = nextState.activeCaptureGroupId;
      queue.value.push(photo);
      queue.value.sort((left, right) => left.position - right.position);
      previewURLs.value[clientPhotoId] = URL.createObjectURL(normalized);
      pumpUploads();
    } catch (error) {
      console.error(error);
      toast.error(t("ai_capture.errors.prepare_photo"));
    }
  }

  function capturePhoto(file: File) {
    captureChain = captureChain.then(() => storeCapture(file));
  }

  async function rolloverCaptureBatch() {
    if (!activeSession.value || rollingOver.value || activeCaptureGroupId.value) return false;
    rollingOver.value = true;
    const previousSession = activeSession.value;
    const locationId = previousSession.location?.id;
    try {
      pumpUploads();
      while (inFlight.size > 0) await Promise.all([...inFlight.values()]);
      if (queue.value.length > 0) {
        toast.error(t("ai_capture.errors.uploads_pending"));
        return false;
      }
      const latestResponse = await api.aiCapture.getSession(previousSession.id);
      if (latestResponse.error) throw new Error(responseMessage(latestResponse));
      const finishedResponse = await api.aiCapture.finishSession(
        previousSession.id,
        latestResponse.data.photos.length,
        latestResponse.data.captureRevision
      );
      if (finishedResponse.error) throw new Error(responseMessage(finishedResponse));
      storeSession(finishedResponse.data);
      await clearCaptureQueueSession(previousSession.id);
      if (!locationId) throw new Error("The capture location is unavailable");
      const nextResponse = await api.aiCapture.createSession(locationId);
      if (nextResponse.error) throw new Error(responseMessage(nextResponse));
      revokePreviews();
      queue.value = [];
      nextPosition.value = 0;
      activeCaptureGroupId.value = undefined;
      replaceSession(nextResponse.data);
      await persistCaptureState();
      toast.success(t("ai_capture.camera.batch_continued"));
      return true;
    } catch (error) {
      console.error(error);
      toast.error(t("ai_capture.errors.rollover_failed"));
      return false;
    } finally {
      rollingOver.value = false;
    }
  }

  async function uploadPhoto(photo: CaptureQueuePhoto) {
    photo.status = "uploading";
    photo.error = undefined;
    await putCaptureQueuePhoto(photo);
    for (let attempt = photo.attempts; attempt < 3; attempt += 1) {
      photo.attempts = attempt + 1;
      await putCaptureQueuePhoto(photo);
      try {
        const response = await api.aiCapture.uploadSessionPhoto(
          photo.sessionId,
          photo.clientPhotoId,
          photo.position,
          photo.blob,
          photo.name,
          photo.captureGroupId || undefined
        );
        if (response.error) throw new Error(responseMessage(response));
        await deleteCaptureQueuePhoto(photo.sessionId, photo.clientPhotoId);
        queue.value = queue.value.filter(candidate => candidate.clientPhotoId !== photo.clientPhotoId);
        const preview = previewURLs.value[photo.clientPhotoId];
        if (preview) URL.revokeObjectURL(preview);
        Reflect.deleteProperty(previewURLs.value, photo.clientPhotoId);
        if (activeSession.value) {
          activeSession.value.photos = [
            ...activeSession.value.photos.filter(candidate => candidate.id !== response.data.id),
            response.data,
          ].sort((left, right) => left.position - right.position);
          activeSession.value.uploadedPhotoCount = activeSession.value.photos.length;
          activeSession.value.photoCount = activeSession.value.photos.length;
        }
        return;
      } catch (error) {
        photo.error = error instanceof Error ? error.message : String(error);
        if (attempt < 2) await new Promise(resolve => setTimeout(resolve, 350 * 2 ** attempt));
      }
    }
    photo.status = "failed";
    await putCaptureQueuePhoto(photo);
  }

  function pumpUploads() {
    if (!activeSession.value) return;
    while (inFlight.size < 2) {
      const photo = queue.value.find(
        candidate => candidate.status === "pending" && !inFlight.has(candidate.clientPhotoId)
      );
      if (!photo) break;
      const operation = uploadPhoto(photo).finally(() => {
        inFlight.delete(photo.clientPhotoId);
        pumpUploads();
      });
      inFlight.set(photo.clientPhotoId, operation);
    }
  }

  async function retryUploads() {
    for (const photo of failedUploads.value) {
      photo.status = "pending";
      photo.attempts = 0;
      photo.error = undefined;
      await putCaptureQueuePhoto(photo);
    }
    pumpUploads();
  }

  async function removeQueuedPhoto(photo: CaptureQueuePhoto) {
    if (photo.status === "uploading") return;
    await deleteCaptureQueuePhoto(photo.sessionId, photo.clientPhotoId);
    queue.value = queue.value.filter(candidate => candidate.clientPhotoId !== photo.clientPhotoId);
    const preview = previewURLs.value[photo.clientPhotoId];
    if (preview) URL.revokeObjectURL(preview);
    Reflect.deleteProperty(previewURLs.value, photo.clientPhotoId);
    await clearEmptyActiveGroup();
  }

  async function removeServerPhoto(photo: AICaptureSessionPhoto) {
    if (!activeSession.value) return;
    const response = await api.aiCapture.deleteSessionPhoto(activeSession.value.id, photo.id);
    if (response.error) {
      toast.error(t("ai_capture.errors.delete_photo"));
      return;
    }
    activeSession.value.photos = activeSession.value.photos.filter(candidate => candidate.id !== photo.id);
    activeSession.value.uploadedPhotoCount = activeSession.value.photos.length;
    activeSession.value.photoCount = activeSession.value.photos.length;
    await clearEmptyActiveGroup();
  }

  async function clearEmptyActiveGroup() {
    if (!activeCaptureGroupId.value || activeGroupViewCount.value > 0) return;
    activeCaptureGroupId.value = undefined;
    await persistCaptureState();
  }

  async function updateSameItemMode(value: boolean) {
    sameItemMode.value = value;
    activeCaptureGroupId.value = undefined;
    await persistCaptureState();
    if (capturedCount.value >= maxSessionPhotos.value) await rolloverCaptureBatch();
  }

  async function nextItem() {
    activeCaptureGroupId.value = undefined;
    await persistCaptureState();
    if (capturedCount.value >= maxSessionPhotos.value) await rolloverCaptureBatch();
  }

  function selectedCaptureGroup(value: string) {
    if (value === "__new__") return createCaptureID();
    return value || null;
  }

  async function regroupQueuedPhoto(photo: CaptureQueuePhoto, value: string) {
    if (photo.status === "uploading") return;
    photo.captureGroupId = selectedCaptureGroup(value);
    await putCaptureQueuePhoto(photo);
    await clearEmptyActiveGroup();
  }

  async function regroupServerPhoto(photo: AICaptureSessionPhoto, value: string) {
    if (!activeSession.value || regroupingPhotoIds.value.includes(photo.id)) return;
    regroupingPhotoIds.value.push(photo.id);
    try {
      const response = await api.aiCapture.updateSessionPhotoGroup(
        activeSession.value.id,
        photo.id,
        selectedCaptureGroup(value)
      );
      if (response.error) throw new Error(responseMessage(response));
      const index = activeSession.value.photos.findIndex(candidate => candidate.id === photo.id);
      if (index >= 0) activeSession.value.photos[index] = response.data;
      await clearEmptyActiveGroup();
    } catch (error) {
      console.error(error);
      toast.error(t("ai_capture.errors.group_photo"));
    } finally {
      regroupingPhotoIds.value = regroupingPhotoIds.value.filter(id => id !== photo.id);
    }
  }

  async function finishCapture() {
    if (!activeSession.value) return;
    loading.value = true;
    view.value = "processing";
    try {
      await captureChain;
      await nextItem();
      pumpUploads();
      while (inFlight.size > 0) await Promise.all([...inFlight.values()]);
      if (queue.value.length > 0) {
        view.value = "camera";
        toast.error(t("ai_capture.errors.uploads_pending"));
        return;
      }
      const latestResponse = await api.aiCapture.getSession(activeSession.value.id);
      if (latestResponse.error) throw new Error(responseMessage(latestResponse));
      replaceSession(latestResponse.data);
      const expected = latestResponse.data.photos.length;
      const response = await api.aiCapture.finishSession(
        latestResponse.data.id,
        expected,
        latestResponse.data.captureRevision
      );
      if (response.error) throw new Error(responseMessage(response));
      replaceSession(response.data);
      view.value = "processing";
    } catch (error) {
      console.error(error);
      view.value = "camera";
      toast.error(t("ai_capture.errors.finish_session"));
    } finally {
      loading.value = false;
    }
  }

  async function retryAnalysis() {
    if (!activeSession.value) return;
    loading.value = true;
    const response = await api.aiCapture.retryAnalysis(activeSession.value.id);
    loading.value = false;
    if (response.error) {
      toast.error(t("ai_capture.errors.analysis_failed"));
      return;
    }
    replaceSession(response.data);
  }

  function addItem() {
    if (!activeSession.value?.draft) return;
    activeSession.value.draft.items.push({
      clientId: createCaptureID(),
      name: "",
      quantity: 1,
      description: "",
      manufacturer: "",
      modelNumber: "",
      entityTypeId: itemTypes.value[0]?.id || "",
      tagIds: [],
      photoIndexes: [],
      photoIds: sessionPhotos.value.map(photo => photo.id),
      moveDisposition: "undecided",
      moveDispositionNote: "",
      needsReview: true,
      reviewReason: t("ai_capture.manual_item"),
    });
  }

  function removeItem(index: number) {
    const item = activeSession.value?.draft?.items[index];
    if (item) clearReanalysis(item.clientId);
    activeSession.value?.draft?.items.splice(index, 1);
  }

  function togglePhoto(item: AICaptureItem, photoId: string, checked: boolean) {
    const current = item.photoIds || [];
    item.photoIds = checked ? [...new Set([...current, photoId])] : current.filter(id => id !== photoId);
    item.captureGroupId = undefined;
    clearReanalysis(item.clientId);
  }

  function splitItem(index: number) {
    const items = activeSession.value?.draft?.items;
    const item = items?.[index];
    if (!items || !item || (item.photoIds?.length || 0) < 2) return;
    const photoIds = item.photoIds || [];
    const splitAt = Math.ceil(photoIds.length / 2);
    const clone = JSON.parse(JSON.stringify(item)) as AICaptureItem;
    clearReanalysis(item.clientId);
    item.photoIds = photoIds.slice(0, splitAt);
    item.captureGroupId = undefined;
    item.needsReview = true;
    item.reviewReason = t("ai_capture.review.split_reason");
    clone.clientId = createCaptureID();
    clone.photoIds = photoIds.slice(splitAt);
    clone.captureGroupId = undefined;
    clone.needsReview = true;
    clone.reviewReason = t("ai_capture.review.split_reason");
    items.splice(index + 1, 0, clone);
  }

  function mergeWithPrevious(index: number) {
    const items = activeSession.value?.draft?.items;
    if (!items || index <= 0 || !items[index]) return;
    const target = items[index - 1];
    const source = items[index];
    if (!target || !source) return;
    clearReanalysis(target.clientId);
    clearReanalysis(source.clientId);
    target.photoIds = [...new Set([...(target.photoIds || []), ...(source.photoIds || [])])];
    target.captureGroupId = undefined;
    target.needsReview = true;
    target.reviewReason = t("ai_capture.review.merge_reason");
    items.splice(index, 1);
  }

  function clearReanalysis(clientId: string) {
    const shouldPersist =
      activeSession.value?.reanalysis?.status === "completed" &&
      (!!activeSession.value.reanalysis.suggestions[clientId] || !!activeSession.value.reanalysis.errors[clientId]);
    const suggestions = { ...reanalysisSuggestions.value };
    const errors = { ...reanalysisErrors.value };
    Reflect.deleteProperty(suggestions, clientId);
    Reflect.deleteProperty(errors, clientId);
    reanalysisSuggestions.value = suggestions;
    reanalysisErrors.value = errors;
    selectedReviewItemIds.value = selectedReviewItemIds.value.filter(id => id !== clientId);
    if (shouldPersist && activeSession.value) {
      const sessionId = activeSession.value.id;
      reanalysisDismissChain = reanalysisDismissChain.then(async () => {
        const response = await api.aiCapture.dismissReanalysis(sessionId, clientId);
        if (!response.error && activeSession.value?.id === sessionId) {
          activeSession.value.reanalysis = response.data.reanalysis;
          syncReanalysis(activeSession.value);
        }
      });
    }
  }

  function cloneDraft(value: AICaptureDraft) {
    return JSON.parse(JSON.stringify(value)) as AICaptureDraft;
  }

  function rememberDraftForUndo() {
    if (activeSession.value?.draft) undoDraft.value = cloneDraft(activeSession.value.draft);
  }

  function reanalysisFieldLabel(field: ReanalysisFieldKey) {
    const keys: Record<ReanalysisFieldKey, string> = {
      name: "global.name",
      quantity: "global.quantity",
      entityTypeId: "global.type",
      manufacturer: "ai_capture.review.manufacturer",
      modelNumber: "ai_capture.review.model",
      description: "ai_capture.review.description_label",
      tagIds: "global.tags",
    };
    return t(keys[field]);
  }

  function reanalysisValuesEqual(field: ReanalysisFieldKey, current: AICaptureItem, suggested: AICaptureItem) {
    if (field === "tagIds") {
      return JSON.stringify([...current.tagIds].sort()) === JSON.stringify([...suggested.tagIds].sort());
    }
    return current[field] === suggested[field];
  }

  function reanalysisChanges(current: AICaptureItem) {
    const suggested = reanalysisSuggestions.value[current.clientId]?.item;
    if (!suggested) return [];
    return reanalysisFieldKeys
      .filter(field => !reanalysisValuesEqual(field, current, suggested))
      .map(field => ({ field, label: reanalysisFieldLabel(field) }));
  }

  function formatReanalysisValue(field: ReanalysisFieldKey, value: AICaptureItem[ReanalysisFieldKey]) {
    if (field === "entityTypeId") {
      return itemTypes.value.find(itemType => itemType.id === value)?.name || String(value || t("global.unknown"));
    }
    if (field === "tagIds") {
      const ids = value as string[];
      return ids.length ? ids.map(id => tags.value.find(tag => tag.id === id)?.name || id).join(", ") : "—";
    }
    return String(value || "—");
  }

  function applySuggestedField(item: AICaptureItem, field: ReanalysisFieldKey) {
    const suggested = reanalysisSuggestions.value[item.clientId]?.item;
    if (!suggested) return;
    rememberDraftForUndo();
    switch (field) {
      case "name":
        item.name = suggested.name;
        break;
      case "quantity":
        item.quantity = suggested.quantity;
        break;
      case "entityTypeId":
        item.entityTypeId = suggested.entityTypeId;
        break;
      case "manufacturer":
        item.manufacturer = suggested.manufacturer;
        break;
      case "modelNumber":
        item.modelNumber = suggested.modelNumber;
        break;
      case "description":
        item.description = suggested.description;
        break;
      case "tagIds":
        item.tagIds = [...suggested.tagIds];
        break;
    }
  }

  function applyAllSuggestedFields(item: AICaptureItem) {
    const suggested = reanalysisSuggestions.value[item.clientId]?.item;
    if (!suggested) return;
    rememberDraftForUndo();
    item.name = suggested.name;
    item.quantity = suggested.quantity;
    item.entityTypeId = suggested.entityTypeId;
    item.manufacturer = suggested.manufacturer;
    item.modelNumber = suggested.modelNumber;
    item.description = suggested.description;
    item.tagIds = [...suggested.tagIds];
    item.needsReview = suggested.needsReview;
    item.reviewReason = suggested.reviewReason;
    clearReanalysis(item.clientId);
    toast.success(t("ai_capture.review.suggestion_applied"));
  }

  function dismissSuggestion(clientId: string) {
    clearReanalysis(clientId);
  }

  function undoLastReanalysis() {
    if (!activeSession.value || !undoDraft.value) return;
    activeSession.value.draft = cloneDraft(undoDraft.value);
    undoDraft.value = null;
    toast.success(t("ai_capture.review.undo_applied"));
  }

  function validDraft(value: AICaptureDraft | undefined, notify = true) {
    if (!value?.items.length) {
      if (notify) toast.error(t("ai_capture.errors.items_required"));
      return false;
    }
    if (value.items.some(item => !item.name.trim() || !item.entityTypeId || item.quantity <= 0)) {
      if (notify) toast.error(t("ai_capture.errors.review_required"));
      return false;
    }
    return true;
  }

  function scheduleAutosave() {
    if (autosaveTimer) clearTimeout(autosaveTimer);
    autosaveTimer = setTimeout(() => {
      if (reanalyzingItemIds.value.length > 0) {
        scheduleAutosave();
      } else if (view.value === "review" && !saving.value && !submitting.value) {
        void saveDraft(false, true);
      }
    }, 1200);
  }

  async function saveDraft(showToast = false, automatic = false) {
    if (saving.value || !activeSession.value?.draft || !validDraft(activeSession.value.draft, !automatic)) {
      return false;
    }
    if (autosaveTimer) clearTimeout(autosaveTimer);
    const sessionId = activeSession.value.id;
    const revision = activeSession.value.draftRevision;
    const sentDraft = JSON.parse(JSON.stringify(activeSession.value.draft)) as AICaptureDraft;
    const sentJSON = JSON.stringify(sentDraft);
    saving.value = true;
    try {
      const response = await api.aiCapture.saveDraft(sessionId, revision, sentDraft);
      if (response.error) throw new Error(responseMessage(response));
      const currentJSON = activeSession.value?.id === sessionId ? JSON.stringify(activeSession.value.draft) : sentJSON;
      if (currentJSON !== sentJSON && activeSession.value?.draft) response.data.draft = activeSession.value.draft;
      replaceSession(response.data);
      if (currentJSON !== sentJSON) scheduleAutosave();
      if (showToast) toast.success(t("ai_capture.review.saved"));
      return true;
    } catch (error) {
      console.error(error);
      toast.error(t("ai_capture.errors.save_draft"));
      return false;
    } finally {
      saving.value = false;
    }
  }

  async function askAI() {
    if (!activeSession.value || !correction.value.trim()) return;
    if (sessionPhotos.value.length > maxProviderPhotos.value) {
      reanalysisProvider.value = "default";
      reanalysisInstruction.value = correction.value.trim();
      const queued = await reanalyzeItems(reviewItems.value.map(({ item }) => item.clientId));
      if (queued) correction.value = "";
      return;
    }
    if (!(await saveDraft())) return;
    const previousDraft = activeSession.value.draft ? cloneDraft(activeSession.value.draft) : null;
    saving.value = true;
    const response = await api.aiCapture.correctSession(
      activeSession.value.id,
      activeSession.value.draftRevision,
      correction.value.trim()
    );
    saving.value = false;
    if (response.error) {
      toast.error(t("ai_capture.errors.analysis_failed"));
      return;
    }
    replaceSession(response.data);
    undoDraft.value = previousDraft;
    reanalysisSuggestions.value = {};
    reanalysisErrors.value = {};
    correction.value = "";
    toast.success(t("ai_capture.correction_applied"));
  }

  async function reanalyzeItems(clientIds: string[]) {
    const uniqueIds = [...new Set(clientIds)];
    if (!activeSession.value || uniqueIds.length === 0 || reanalysisActive.value || !(await saveDraft())) return false;
    const sessionId = activeSession.value.id;
    const revision = activeSession.value.draftRevision;
    const provider = reanalysisProvider.value;
    const selected = uniqueIds.filter(clientId =>
      activeSession.value?.draft?.items.some(item => item.clientId === clientId)
    );
    const response = await api.aiCapture.queueReanalysis(
      sessionId,
      revision,
      selected,
      provider,
      reanalysisInstruction.value.trim()
    );
    if (response.error) {
      toast.error(responseMessage(response));
      return false;
    }
    replaceSession(response.data);
    toast.success(t("ai_capture.review.reanalysis_queued", { count: selected.length }));
    return true;
  }

  function reanalyzeItem(item: AICaptureItem) {
    return reanalyzeItems([item.clientId]);
  }

  async function submitReviewItems(clientIds: string[]) {
    const selected = [...new Set(clientIds)].filter(clientId =>
      reviewItems.value.some(({ item }) => item.clientId === clientId)
    );
    if (!activeSession.value || selected.length === 0 || reanalysisActive.value || !(await saveDraft())) return;
    submitting.value = true;
    const response = await api.aiCapture.submitItems(
      activeSession.value.id,
      activeSession.value.draftRevision,
      selected
    );
    submitting.value = false;
    if (response.error || response.data.errorCode) {
      if (!response.error) replaceSession(response.data);
      toast.error(response.data.errorMessage || t("ai_capture.errors.partial_submit"));
      return;
    }
    replaceSession(response.data);
    await locationStore.refreshChildren();
    selectedReviewItemIds.value = selectedReviewItemIds.value.filter(clientId => !selected.includes(clientId));
    if (response.data.status === "completed") {
      await clearCaptureQueueSession(response.data.id);
      view.value = "done";
    }
    toast.success(t("ai_capture.review.items_created", { count: selected.length }));
  }

  async function deleteSession(session: AICaptureSession) {
    if (!confirm(t("ai_capture.sessions.delete_confirm"))) return;
    const response = await api.aiCapture.deleteSession(session.id);
    if (response.error) {
      toast.error(t("ai_capture.errors.delete_session"));
      return;
    }
    await clearCaptureQueueSession(session.id);
    sessions.value = sessions.value.filter(candidate => candidate.id !== session.id);
    if (activeSession.value?.id === session.id) {
      activeSession.value = null;
      view.value = "sessions";
    }
  }

  async function pollSessions() {
    await loadSessions();
    if (!activeSession.value) return;
    const current = sessions.value.find(session => session.id === activeSession.value?.id);
    if (!current) return;
    if (view.value === "processing") {
      replaceSession(current);
      if (current.status === "ready_for_review") view.value = "review";
      if (current.status === "completed") view.value = "done";
    } else if (view.value === "review") {
      const previousStatus = activeSession.value.reanalysis?.status;
      activeSession.value.reanalysis = current.reanalysis;
      syncReanalysis(activeSession.value);
      if (reanalysisActiveStatus(previousStatus) && current.reanalysis?.status === "completed") {
        const succeeded = current.reanalysis.clientIds.filter(
          clientId => !!current.reanalysis?.suggestions[clientId]
        ).length;
        const failed = current.reanalysis.clientIds.filter(clientId => !!current.reanalysis?.errors[clientId]).length;
        if (succeeded) toast.success(t("ai_capture.review.reanalysis_complete", { count: succeeded }));
        if (failed) toast.error(t("ai_capture.review.reanalysis_failed", { count: failed }));
      }
    }
  }

  async function showSessions() {
    if (view.value === "review" && activeSession.value?.draft && !(await saveDraft())) return;
    view.value = "sessions";
  }

  watch(
    () => (activeSession.value?.draft ? JSON.stringify(activeSession.value.draft) : ""),
    (value, previous) => {
      if (view.value === "review" && value && previous && value !== previous) scheduleAutosave();
    }
  );

  onMounted(async () => {
    await Promise.all([entityTypeStore.ensureFetched(), tagStore.ensureAllTagsFetched()]);
    const [, statusResponse] = await Promise.all([loadSessions(), publicApi.status()]);
    if (!statusResponse.error) status.value = statusResponse.data;
    pollTimer = setInterval(() => void pollSessions(), 2500);
  });

  onBeforeUnmount(() => {
    if (pollTimer) clearInterval(pollTimer);
    if (autosaveTimer) clearTimeout(autosaveTimer);
    revokePreviews();
  });
</script>

<template>
  <BaseContainer class="max-w-5xl pb-24">
    <header class="mb-5 flex flex-wrap items-center justify-between gap-3">
      <div class="flex items-center gap-3">
        <div class="rounded-xl bg-primary p-3 text-primary-foreground">
          <MdiMagicStaff class="size-7" />
        </div>
        <div>
          <h1 class="text-2xl font-bold sm:text-3xl">
            {{ $t("ai_capture.title") }}
          </h1>
          <p class="text-sm text-muted-foreground">
            {{ $t("ai_capture.subtitle_sessions") }}
          </p>
        </div>
      </div>
      <Button v-if="view !== 'sessions'" variant="outline" @click="showSessions">
        <MdiArrowLeft class="mr-2" /> {{ $t("ai_capture.sessions.title") }}
      </Button>
    </header>

    <div
      v-if="status && !status.ai.enabled"
      class="mb-4 flex gap-3 rounded-lg border border-destructive bg-destructive/10 p-4 text-sm"
    >
      <MdiAlertCircleOutline class="size-5 shrink-0 text-destructive" />
      <p>{{ $t("ai_capture.disabled_help") }}</p>
    </div>

    <div v-if="view === 'sessions'" class="space-y-4">
      <Card>
        <CardContent class="flex flex-wrap items-center justify-between gap-4 py-5">
          <div>
            <h2 class="font-semibold">{{ $t("ai_capture.sessions.new_title") }}</h2>
            <p class="text-sm text-muted-foreground">{{ $t("ai_capture.sessions.new_description") }}</p>
          </div>
          <Button :disabled="!aiEnabled" @click="newSession">
            <MdiPlus class="mr-2" /> {{ $t("ai_capture.sessions.new") }}
          </Button>
        </CardContent>
      </Card>

      <div class="grid grid-cols-2 gap-2 rounded-lg bg-muted p-1">
        <Button :variant="landingTab === 'review' ? 'default' : 'ghost'" @click="landingTab = 'review'">
          <MdiPackageVariant class="mr-2" />
          {{ $t("ai_capture.pending.title", { count: pendingReviewItems.length }) }}
        </Button>
        <Button :variant="landingTab === 'activity' ? 'default' : 'ghost'" @click="landingTab = 'activity'">
          <MdiClockOutline class="mr-2" /> {{ $t("ai_capture.activity.title") }}
        </Button>
      </div>

      <template v-if="landingTab === 'review'">
        <Card v-if="pendingReviewItems.length">
          <CardContent class="space-y-3 py-4">
            <div class="flex flex-wrap items-center justify-between gap-3">
              <label class="flex items-center gap-2 text-sm font-medium">
                <Checkbox
                  :model-value="allPendingItemsSelected"
                  @update:model-value="value => toggleAllPendingItems(value === true)"
                />
                {{ $t("ai_capture.pending.select_all") }}
              </label>
              <Button
                :disabled="selectedPendingItems.length === 0 || submitting"
                @click="submitPendingItems(selectedPendingItems)"
              >
                <MdiLoading v-if="submitting" class="mr-2 animate-spin" />
                <MdiCheckCircle v-else class="mr-2" />
                {{ $t("ai_capture.pending.create_selected", { count: selectedPendingItems.length }) }}
              </Button>
            </div>
            <div class="grid gap-2 sm:grid-cols-[12rem_auto] sm:items-end">
              <div class="space-y-1">
                <Label for="pending-move-disposition">{{ $t("ai_capture.review.move_disposition") }}</Label>
                <Select id="pending-move-disposition" v-model="pendingBulkMoveDisposition">
                  <SelectTrigger><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem v-for="disposition in moveDispositionOptions" :key="disposition" :value="disposition">
                      {{ moveDispositionLabel(disposition) }}
                    </SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <Button
                variant="outline"
                :disabled="selectedPendingItems.length === 0 || saving || submitting"
                @click="applyPendingBulkMoveDisposition"
              >
                {{ $t("ai_capture.review.move_apply_selected", { count: selectedPendingItems.length }) }}
              </Button>
            </div>
          </CardContent>
        </Card>

        <div v-if="pendingReviewItems.length" class="grid gap-3 md:grid-cols-2">
          <Card v-for="entry in pendingReviewItems" :key="entry.key" class="overflow-hidden">
            <CardHeader class="pb-3">
              <div class="flex items-start gap-3">
                <Checkbox
                  class="mt-1"
                  :model-value="selectedPendingItemKeys.includes(entry.key)"
                  :aria-label="$t('ai_capture.review.select_item', { name: entry.item.name })"
                  @update:model-value="value => togglePendingItem(entry.key, value === true)"
                />
                <div class="min-w-0 grow">
                  <CardTitle class="truncate text-lg">{{
                    entry.item.name || $t("ai_capture.review.unnamed")
                  }}</CardTitle>
                  <CardDescription class="flex items-center gap-1">
                    <MdiMapMarker /> {{ entry.session.location?.name || $t("ai_capture.sessions.location_removed") }}
                  </CardDescription>
                </div>
              </div>
            </CardHeader>
            <CardContent class="space-y-3">
              <div v-if="pendingItemPhotos(entry).length" class="flex gap-2 overflow-x-auto">
                <img
                  v-for="photo in pendingItemPhotos(entry).slice(0, 4)"
                  :key="photo.id"
                  :src="api.aiCapture.photoURL(entry.session.id, photo.id)"
                  :alt="photo.originalName"
                  class="size-20 shrink-0 rounded-md object-cover"
                />
              </div>
              <div class="flex flex-wrap gap-2 text-xs">
                <span class="rounded-full bg-muted px-2 py-1"
                  >{{ $t("global.quantity") }}: {{ entry.item.quantity }}</span
                >
                <span class="rounded-full bg-muted px-2 py-1">
                  {{ moveDispositionLabel(entry.item.moveDisposition) }}
                </span>
                <span v-if="entry.item.captureGroupId" class="rounded-full bg-primary/10 px-2 py-1 text-primary">
                  <MdiImageMultiple class="mr-1 inline" /> {{ $t("ai_capture.pending.multiple_views") }}
                </span>
              </div>
              <p v-if="entry.item.needsReview" class="text-sm text-amber-600">
                {{ entry.item.reviewReason || $t("ai_capture.review.check_item") }}
              </p>
              <div class="flex justify-end gap-2">
                <Button size="sm" variant="outline" @click="openPendingItem(entry)">
                  {{ $t("ai_capture.pending.review_item") }}
                </Button>
                <Button size="sm" :disabled="submitting" @click="submitPendingItems([entry])">
                  {{ $t("ai_capture.pending.create_item") }}
                </Button>
              </div>
            </CardContent>
          </Card>
        </div>
        <div v-else class="rounded-xl border border-dashed p-10 text-center text-muted-foreground">
          <MdiCheckCircle class="mx-auto mb-3 size-12" />
          <p>{{ $t("ai_capture.pending.empty") }}</p>
        </div>
      </template>

      <template v-else>
        <div v-if="activitySessions.length" class="grid gap-3 md:grid-cols-2">
          <Card v-for="session in activitySessions" :key="session.id" class="overflow-hidden">
            <CardHeader class="pb-3">
              <div class="flex items-start justify-between gap-3">
                <div class="min-w-0">
                  <CardTitle class="truncate text-lg">{{
                    session.location?.name || $t("ai_capture.sessions.location_removed")
                  }}</CardTitle>
                  <CardDescription>{{ sessionTime(session.updatedAt) }}</CardDescription>
                </div>
                <span class="rounded-full px-2.5 py-1 text-xs font-medium" :class="statusClass(session.status)">
                  {{ statusLabel(session.status) }}
                </span>
              </div>
            </CardHeader>
            <CardContent class="space-y-3">
              <div class="flex items-center justify-between text-sm text-muted-foreground">
                <span
                  ><MdiImageMultiple class="mr-1 inline" />
                  {{ $t("ai_capture.sessions.photos", { count: session.photoCount }) }}</span
                >
                <span v-if="session.draft">{{
                  $t("ai_capture.sessions.items", { count: session.draft.items.length })
                }}</span>
              </div>
              <p v-if="session.errorMessage" class="text-sm text-destructive">{{ session.errorMessage }}</p>
              <div class="flex justify-between gap-2">
                <Button size="sm" variant="ghost" class="text-destructive" @click="deleteSession(session)">
                  <MdiDelete class="mr-1" /> {{ $t("global.delete") }}
                </Button>
                <Button size="sm" :disabled="loading" @click="openSession(session)">
                  {{
                    session.status === "capturing" ? $t("ai_capture.sessions.resume") : $t("ai_capture.sessions.open")
                  }}
                </Button>
              </div>
            </CardContent>
          </Card>
        </div>
        <div v-else class="rounded-xl border border-dashed p-10 text-center text-muted-foreground">
          <MdiCamera class="mx-auto mb-3 size-12" />
          <p>{{ $t("ai_capture.activity.empty") }}</p>
        </div>
      </template>
    </div>

    <Card v-else-if="view === 'location'">
      <CardHeader>
        <CardTitle class="flex items-center gap-2"><MdiMapMarker /> {{ $t("ai_capture.location.title") }}</CardTitle>
        <CardDescription>{{ $t("ai_capture.location.description") }}</CardDescription>
      </CardHeader>
      <CardContent class="space-y-4">
        <LocationCapturePicker v-model="selectedLocation" />
        <div class="flex justify-end">
          <Button :disabled="!selectedLocation || loading" @click="startCapturing">
            <MdiLoading v-if="loading" class="mr-2 animate-spin" />
            <MdiCamera v-else class="mr-2" />
            {{ $t("ai_capture.location.start_camera") }}
          </Button>
        </div>
      </CardContent>
    </Card>

    <div v-else-if="view === 'camera' && activeSession" class="space-y-3">
      <div class="flex items-center justify-between gap-3 rounded-lg border bg-muted/30 p-3">
        <div class="flex min-w-0 items-center gap-2">
          <MdiMapMarker class="size-5 shrink-0 text-primary" />
          <strong class="truncate">{{ activeSession.location?.name }}</strong>
        </div>
        <Button variant="ghost" size="sm" @click="view = 'location'">{{ $t("global.update") }}</Button>
      </div>
      <AICaptureCamera
        :count="capturedCount"
        :limit-reached="
          !!activeCaptureGroupId && (activeGroupViewCount >= maxProviderPhotos || capturedCount >= maxSessionPhotos)
        "
        :busy="loading || rollingOver"
        :same-item-mode="sameItemMode"
        :active-group-number="activeGroupNumber"
        :active-group-view-count="activeGroupViewCount"
        @captured="capturePhoto"
        @finish="finishCapture"
        @next-item="nextItem"
        @update:same-item-mode="updateSameItemMode"
      />

      <div
        v-if="queuedUploads || failedUploads.length"
        class="flex flex-wrap items-center justify-between gap-2 rounded-lg border p-3 text-sm"
      >
        <span v-if="queuedUploads"
          ><MdiUpload class="mr-1 inline" /> {{ $t("ai_capture.camera.uploading", { count: queuedUploads }) }}</span
        >
        <span v-if="failedUploads.length" class="text-destructive">{{
          $t("ai_capture.camera.upload_failed", { count: failedUploads.length })
        }}</span>
        <Button v-if="failedUploads.length" size="sm" variant="outline" @click="retryUploads"
          ><MdiRefresh class="mr-1" /> {{ $t("global.retry") }}</Button
        >
      </div>

      <div v-if="capturedCount" class="flex gap-2 overflow-x-auto rounded-lg border p-2">
        <div v-for="photo in activeSession.photos" :key="photo.id" class="w-24 shrink-0 space-y-1">
          <div class="group relative">
            <img
              :src="api.aiCapture.photoURL(activeSession.id, photo.id)"
              :alt="photo.originalName"
              class="size-24 rounded-md object-cover"
            />
            <span class="absolute bottom-1 left-1 rounded bg-black/75 px-1.5 py-0.5 text-[10px] text-white">
              {{ groupLabel(photo.captureGroupId) }}
            </span>
            <button
              type="button"
              class="absolute right-1 top-1 rounded-full bg-black/70 p-1 text-white"
              :aria-label="$t('ai_capture.camera.delete_photo')"
              @click="removeServerPhoto(photo)"
            >
              <MdiDelete />
            </button>
          </div>
          <label :for="`server-photo-group-${photo.id}`" class="sr-only">
            {{ $t("ai_capture.camera.photo_group") }}
          </label>
          <select
            :id="`server-photo-group-${photo.id}`"
            class="h-8 w-24 rounded border bg-background px-1 text-xs text-foreground"
            :value="photo.captureGroupId || ''"
            :disabled="regroupingPhotoIds.includes(photo.id)"
            @change="regroupServerPhoto(photo, ($event.target as HTMLSelectElement).value)"
          >
            <option value="">{{ $t("ai_capture.camera.ungrouped") }}</option>
            <option v-for="group in captureGroups" :key="group.id" :value="group.id">
              {{ $t("ai_capture.camera.item_group", { group: group.number }) }}
            </option>
            <option value="__new__">
              {{ $t("ai_capture.camera.new_item_group") }}
            </option>
          </select>
        </div>
        <div v-for="photo in queue" :key="photo.clientPhotoId" class="w-24 shrink-0 space-y-1">
          <div class="group relative">
            <img
              :src="previewURLs[photo.clientPhotoId]"
              :alt="photo.name"
              class="size-24 rounded-md object-cover"
              :class="photo.status === 'failed' ? 'opacity-60' : ''"
            />
            <span class="absolute bottom-1 left-1 rounded bg-black/75 px-1.5 py-0.5 text-[10px] text-white">
              {{ groupLabel(photo.captureGroupId) }}
            </span>
            <MdiLoading
              v-if="photo.status === 'uploading'"
              class="absolute inset-0 m-auto size-7 animate-spin text-white drop-shadow"
            />
            <button
              v-if="photo.status !== 'uploading'"
              type="button"
              class="absolute right-1 top-1 rounded-full bg-black/70 p-1 text-white"
              :aria-label="$t('ai_capture.camera.delete_photo')"
              @click="removeQueuedPhoto(photo)"
            >
              <MdiDelete />
            </button>
          </div>
          <label :for="`queued-photo-group-${photo.clientPhotoId}`" class="sr-only">
            {{ $t("ai_capture.camera.photo_group") }}
          </label>
          <select
            :id="`queued-photo-group-${photo.clientPhotoId}`"
            class="h-8 w-24 rounded border bg-background px-1 text-xs text-foreground"
            :value="photo.captureGroupId || ''"
            :disabled="photo.status === 'uploading'"
            @change="regroupQueuedPhoto(photo, ($event.target as HTMLSelectElement).value)"
          >
            <option value="">{{ $t("ai_capture.camera.ungrouped") }}</option>
            <option v-for="group in captureGroups" :key="group.id" :value="group.id">
              {{ $t("ai_capture.camera.item_group", { group: group.number }) }}
            </option>
            <option value="__new__">
              {{ $t("ai_capture.camera.new_item_group") }}
            </option>
          </select>
        </div>
      </div>
    </div>

    <Card v-else-if="view === 'processing' && activeSession">
      <CardContent class="py-12 text-center">
        <MdiLoading v-if="activeStatus !== 'analysis_failed'" class="mx-auto size-14 animate-spin text-primary" />
        <MdiAlertCircleOutline v-else class="mx-auto size-14 text-destructive" />
        <h2 class="mt-4 text-xl font-bold">{{ statusLabel(activeStatus) }}</h2>
        <p class="mx-auto mt-2 max-w-md text-muted-foreground">
          {{
            activeStatus === "analysis_failed"
              ? activeSession.errorMessage || $t("ai_capture.processing.failed")
              : activeSession.errorMessage || $t("ai_capture.processing.description")
          }}
        </p>
        <Button v-if="activeStatus === 'analysis_failed'" class="mt-5" :disabled="loading" @click="retryAnalysis">
          <MdiRefresh class="mr-2" /> {{ $t("ai_capture.processing.retry") }}
        </Button>
        <p v-else class="mt-5 text-sm text-muted-foreground">
          <MdiClockOutline class="mr-1 inline" />
          {{ $t("ai_capture.processing.leave") }}
        </p>
      </CardContent>
    </Card>

    <div v-else-if="view === 'review' && activeSession && draft" class="space-y-4">
      <Card>
        <CardHeader>
          <CardTitle>{{ $t("ai_capture.review.title") }}</CardTitle>
          <CardDescription>{{ $t("ai_capture.review.description") }}</CardDescription>
        </CardHeader>
        <CardContent class="space-y-3">
          <div class="flex items-center gap-2 rounded-md bg-muted p-3 text-sm">
            <MdiMapMarker class="size-5 shrink-0 text-primary" />
            <span>{{ $t("ai_capture.review.saving_to") }}</span
            ><strong>{{ activeSession.location?.name }}</strong>
          </div>
          <p class="text-sm text-muted-foreground">
            {{ $t("ai_capture.review.ai_scope") }}
          </p>
          <div
            v-for="warning in draft.warnings"
            :key="warning"
            class="flex gap-2 rounded-md bg-amber-500/10 p-3 text-sm"
          >
            <MdiAlertCircleOutline class="size-5 shrink-0 text-amber-600" />
            {{ warning }}
          </div>
          <div class="grid gap-2 sm:grid-cols-[1fr_auto]">
            <Textarea
              v-model="correction"
              :placeholder="$t('ai_capture.review.correction_placeholder')"
              :disabled="saving || reanalysisActive"
            />
            <Button variant="outline" :disabled="!correction.trim() || saving || reanalysisActive" @click="askAI">
              <MdiLoading v-if="saving" class="mr-2 animate-spin" /><MdiMagicStaff v-else class="mr-2" />
              {{ $t("ai_capture.review.ask_ai") }}
            </Button>
          </div>
          <div class="space-y-3 rounded-lg border p-3">
            <div class="flex flex-wrap items-start justify-between gap-2">
              <div>
                <h3 class="font-medium">
                  {{ $t("ai_capture.review.reanalyze_title") }}
                </h3>
                <p class="text-sm text-muted-foreground">
                  {{ $t("ai_capture.review.reanalyze_help") }}
                </p>
              </div>
              <Button v-if="undoDraft" size="sm" variant="outline" @click="undoLastReanalysis">
                {{ $t("ai_capture.review.undo") }}
              </Button>
            </div>
            <div v-if="activeSession.reanalysis && reanalysisActive" class="flex gap-3 rounded-md bg-muted p-3 text-sm">
              <MdiLoading class="mt-0.5 shrink-0 animate-spin" />
              <div>
                <p class="font-medium">
                  {{
                    activeSession.reanalysis.status === "waiting"
                      ? $t("ai_capture.review.reanalysis_waiting", {
                          provider: providerName(activeSession.reanalysis.provider),
                        })
                      : $t("ai_capture.review.reanalysis_processing")
                  }}
                </p>
                <p class="text-muted-foreground">
                  {{
                    $t("ai_capture.review.reanalysis_progress", {
                      completed: activeSession.reanalysis.completed,
                      total: activeSession.reanalysis.total,
                    })
                  }}
                </p>
              </div>
            </div>
            <p v-if="reanalysisErrors._job" class="text-sm text-destructive">
              {{ reanalysisErrors._job }}
            </p>
            <div class="grid gap-2 sm:grid-cols-[12rem_1fr]">
              <div class="space-y-1">
                <Label for="reanalysis-provider">{{ $t("ai_capture.review.provider") }}</Label>
                <Select id="reanalysis-provider" v-model="reanalysisProvider" :disabled="reanalysisActive">
                  <SelectTrigger><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem v-for="provider in reanalysisProviders" :key="provider.id" :value="provider.id">
                      {{ provider.name }}
                    </SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div class="space-y-1">
                <Label for="reanalysis-instruction">{{ $t("ai_capture.review.optional_instruction") }}</Label>
                <Input
                  id="reanalysis-instruction"
                  v-model="reanalysisInstruction"
                  maxlength="2000"
                  :disabled="reanalysisActive"
                  :placeholder="$t('ai_capture.review.reanalysis_placeholder')"
                />
              </div>
            </div>
            <div class="flex flex-wrap items-center justify-between gap-2">
              <label class="flex items-center gap-2 text-sm">
                <Checkbox
                  :model-value="allReviewItemsSelected"
                  @update:model-value="value => toggleAllReviewItems(value === true)"
                />
                {{ $t("ai_capture.review.select_all") }}
              </label>
              <Button
                variant="outline"
                :disabled="selectedReviewItemIds.length === 0 || reanalysisActive || saving"
                @click="reanalyzeItems(selectedReviewItemIds)"
              >
                <MdiLoading v-if="reanalysisActive" class="mr-2 animate-spin" />
                <MdiRefresh v-else class="mr-2" />
                {{
                  $t("ai_capture.review.reanalyze_selected", {
                    count: selectedReviewItemIds.length,
                    provider: providerName(reanalysisProvider),
                  })
                }}
              </Button>
            </div>
          </div>
          <div class="space-y-3 rounded-lg border p-3">
            <div>
              <h3 class="font-medium">
                {{ $t("ai_capture.review.move_plan_title") }}
              </h3>
              <p class="text-sm text-muted-foreground">
                {{ $t("ai_capture.review.move_plan_help") }}
              </p>
            </div>
            <label class="flex items-center gap-2 text-sm">
              <Checkbox
                :model-value="allReviewItemsSelected"
                @update:model-value="value => toggleAllReviewItems(value === true)"
              />
              {{ $t("ai_capture.review.select_all") }}
            </label>
            <div class="grid gap-2 sm:grid-cols-[12rem_auto] sm:items-end sm:justify-start">
              <div class="space-y-1">
                <Label for="bulk-move-disposition">{{ $t("ai_capture.review.move_disposition") }}</Label>
                <Select id="bulk-move-disposition" v-model="bulkMoveDisposition">
                  <SelectTrigger><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem v-for="disposition in moveDispositionOptions" :key="disposition" :value="disposition">
                      {{ moveDispositionLabel(disposition) }}
                    </SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <Button
                variant="outline"
                :disabled="selectedReviewItemIds.length === 0 || saving || submitting"
                @click="applyBulkMoveDisposition"
              >
                {{
                  $t("ai_capture.review.move_apply_selected", {
                    count: selectedReviewItemIds.length,
                  })
                }}
              </Button>
            </div>
          </div>
        </CardContent>
      </Card>

      <Card v-for="{ item, itemIndex } in reviewItems" :id="`review-item-${item.clientId}`" :key="item.clientId">
        <CardHeader class="pb-3">
          <div class="flex flex-wrap items-start justify-between gap-2">
            <div>
              <CardTitle class="flex items-center gap-2 text-lg">
                <Checkbox
                  :model-value="selectedReviewItemIds.includes(item.clientId)"
                  :aria-label="$t('ai_capture.review.select_item', { name: item.name })"
                  @update:model-value="value => toggleReviewItem(item.clientId, value === true)"
                />
                <MdiPackageVariant />
                {{ item.name || $t("ai_capture.review.unnamed") }}
              </CardTitle>
              <span
                v-if="item.captureGroupId"
                class="mt-2 inline-flex items-center rounded-full bg-primary/10 px-2 py-1 text-xs font-medium text-primary"
              >
                <MdiImageMultiple class="mr-1" />
                {{
                  $t("ai_capture.review.multiple_views", {
                    group: groupLabel(item.captureGroupId),
                  })
                }}
              </span>
              <CardDescription v-if="item.needsReview" class="mt-1 text-amber-600">{{
                item.reviewReason || $t("ai_capture.review.check_item")
              }}</CardDescription>
            </div>
            <div class="flex flex-wrap justify-end gap-1">
              <Button
                size="sm"
                :disabled="saving || submitting || reanalysisActive"
                @click="submitReviewItems([item.clientId])"
              >
                <MdiCheckCircle class="mr-1" /> {{ $t("ai_capture.pending.create_item") }}
              </Button>
              <Button
                size="sm"
                variant="outline"
                :disabled="reanalysisActive || saving || !(item.photoIds?.length || 0)"
                @click="reanalyzeItem(item)"
              >
                <MdiLoading v-if="reanalyzingItemIds.includes(item.clientId)" class="mr-1 animate-spin" />
                <MdiRefresh v-else class="mr-1" />
                {{
                  $t("ai_capture.review.reanalyze_with", {
                    provider: providerName(reanalysisProvider),
                  })
                }}
              </Button>
              <Button
                size="sm"
                variant="outline"
                :disabled="(item.photoIds?.length || 0) < 2"
                @click="splitItem(itemIndex)"
              >
                {{ $t("ai_capture.review.split") }}
              </Button>
              <Button
                v-if="itemIndex > 0 && !completedReviewItemIDs.has(draft.items[itemIndex - 1]!.clientId)"
                size="sm"
                variant="outline"
                @click="mergeWithPrevious(itemIndex)"
              >
                {{ $t("ai_capture.review.merge_previous") }}
              </Button>
              <Button variant="ghost" size="icon" @click="removeItem(itemIndex)"><MdiDelete /></Button>
            </div>
          </div>
        </CardHeader>
        <CardContent class="grid gap-4 sm:grid-cols-2">
          <div
            v-if="reanalysisErrors[item.clientId]"
            class="rounded-md border border-destructive bg-destructive/10 p-3 text-sm text-destructive sm:col-span-2"
          >
            {{ reanalysisErrors[item.clientId] }}
          </div>
          <div
            v-if="reanalysisSuggestions[item.clientId]"
            class="space-y-3 rounded-lg border border-primary/40 bg-primary/5 p-3 sm:col-span-2"
          >
            <div class="flex flex-wrap items-start justify-between gap-2">
              <div>
                <h3 class="font-medium">
                  {{
                    $t("ai_capture.review.suggestion_from", {
                      provider: providerName(reanalysisSuggestions[item.clientId]!.provider),
                    })
                  }}
                </h3>
                <p class="text-sm text-muted-foreground">
                  {{ $t("ai_capture.review.suggestion_help") }}
                </p>
              </div>
              <div class="flex gap-2">
                <Button
                  size="sm"
                  variant="ghost"
                  :disabled="reanalysisActive"
                  @click="dismissSuggestion(item.clientId)"
                >
                  {{ $t("ai_capture.review.dismiss") }}
                </Button>
                <Button size="sm" :disabled="reanalysisActive" @click="applyAllSuggestedFields(item)">
                  {{ $t("ai_capture.review.apply_all") }}
                </Button>
              </div>
            </div>
            <p
              v-for="warning in reanalysisSuggestions[item.clientId]!.warnings"
              :key="warning"
              class="text-sm text-amber-700 dark:text-amber-300"
            >
              {{ warning }}
            </p>
            <p v-if="reanalysisChanges(item).length === 0" class="text-sm text-muted-foreground">
              {{ $t("ai_capture.review.no_changes") }}
            </p>
            <div
              v-for="change in reanalysisChanges(item)"
              :key="change.field"
              class="grid gap-2 rounded-md bg-background p-2 sm:grid-cols-[8rem_1fr_1fr_auto] sm:items-center"
            >
              <strong class="text-sm">{{ change.label }}</strong>
              <div class="min-w-0 text-sm">
                <span class="block text-xs text-muted-foreground">{{ $t("ai_capture.review.current") }}</span>
                <span class="break-words">{{ formatReanalysisValue(change.field, item[change.field]) }}</span>
              </div>
              <div class="min-w-0 text-sm">
                <span class="block text-xs text-muted-foreground">{{ $t("ai_capture.review.suggested") }}</span>
                <span class="break-words">
                  {{ formatReanalysisValue(change.field, reanalysisSuggestions[item.clientId]!.item[change.field]) }}
                </span>
              </div>
              <Button
                size="sm"
                variant="outline"
                :disabled="reanalysisActive"
                @click="applySuggestedField(item, change.field)"
              >
                {{ $t("ai_capture.review.use_suggestion") }}
              </Button>
            </div>
          </div>
          <div class="space-y-1">
            <Label :for="`item-name-${itemIndex}`">{{ $t("global.name") }}</Label
            ><Input :id="`item-name-${itemIndex}`" v-model="item.name" maxlength="255" />
          </div>
          <div class="space-y-1">
            <Label :for="`item-quantity-${itemIndex}`">{{ $t("global.quantity") }}</Label
            ><Input
              :id="`item-quantity-${itemIndex}`"
              v-model.number="item.quantity"
              type="number"
              min="0.01"
              step="any"
            />
          </div>
          <div class="space-y-1">
            <Label>{{ $t("global.type") }}</Label>
            <Select v-model="item.entityTypeId"
              ><SelectTrigger><SelectValue :placeholder="$t('global.select')" /></SelectTrigger
              ><SelectContent
                ><SelectItem v-for="entityType in itemTypes" :key="entityType.id" :value="entityType.id">{{
                  entityType.name
                }}</SelectItem></SelectContent
              ></Select
            >
          </div>
          <div class="space-y-3 rounded-lg border bg-muted/30 p-3 sm:col-span-2">
            <div>
              <h3 class="font-medium">
                {{ $t("ai_capture.review.move_plan_item_title") }}
              </h3>
              <p class="text-sm text-muted-foreground">
                {{ $t("ai_capture.review.move_plan_item_help") }}
              </p>
            </div>
            <div class="grid gap-3 sm:grid-cols-2">
              <div class="space-y-1">
                <Label :for="`item-move-disposition-${itemIndex}`">
                  {{ $t("ai_capture.review.move_disposition") }}
                </Label>
                <Select :id="`item-move-disposition-${itemIndex}`" v-model="item.moveDisposition">
                  <SelectTrigger><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem v-for="disposition in moveDispositionOptions" :key="disposition" :value="disposition">
                      {{ moveDispositionLabel(disposition) }}
                    </SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div class="space-y-1">
                <Label :for="`item-move-note-${itemIndex}`">
                  {{ moveDispositionNoteLabel(item.moveDisposition) }}
                </Label>
                <Input
                  :id="`item-move-note-${itemIndex}`"
                  v-model="item.moveDispositionNote"
                  maxlength="500"
                  :placeholder="moveDispositionNotePlaceholder(item.moveDisposition)"
                />
              </div>
            </div>
          </div>
          <div class="space-y-1">
            <Label :for="`item-manufacturer-${itemIndex}`">{{ $t("ai_capture.review.manufacturer") }}</Label
            ><Input :id="`item-manufacturer-${itemIndex}`" v-model="item.manufacturer" maxlength="255" />
          </div>
          <div class="space-y-1 sm:col-span-2">
            <Label :for="`item-model-${itemIndex}`">{{ $t("ai_capture.review.model") }}</Label
            ><Input :id="`item-model-${itemIndex}`" v-model="item.modelNumber" maxlength="255" />
          </div>
          <div class="space-y-1 sm:col-span-2">
            <Label :for="`item-description-${itemIndex}`">{{ $t("ai_capture.review.description_label") }}</Label
            ><Textarea :id="`item-description-${itemIndex}`" v-model="item.description" maxlength="1000" />
          </div>
          <div class="sm:col-span-2">
            <TagSelector v-model="item.tagIds" :tags="tags" />
          </div>
          <fieldset class="sm:col-span-2">
            <legend class="mb-2 text-sm font-medium">
              {{ $t("ai_capture.review.photos_for_item") }}
            </legend>
            <div class="flex flex-wrap gap-2">
              <label
                v-for="photo in sessionPhotos"
                :key="photo.id"
                class="relative cursor-pointer overflow-hidden rounded-md border-2"
                :class="(item.photoIds || []).includes(photo.id) ? 'border-primary' : 'border-transparent opacity-50'"
              >
                <img
                  :src="api.aiCapture.photoURL(activeSession.id, photo.id)"
                  :alt="photo.originalName"
                  class="size-20 object-cover"
                />
                <input
                  type="checkbox"
                  class="absolute right-1 top-1"
                  :checked="(item.photoIds || []).includes(photo.id)"
                  @change="togglePhoto(item, photo.id, ($event.target as HTMLInputElement).checked)"
                />
              </label>
            </div>
          </fieldset>
        </CardContent>
      </Card>

      <div class="flex flex-wrap justify-between gap-2">
        <Button variant="outline" :disabled="saving || submitting" @click="addItem"
          ><MdiPlus class="mr-2" /> {{ $t("ai_capture.review.add_item") }}</Button
        >
        <div class="flex gap-2">
          <Button variant="outline" :disabled="saving || submitting" @click="saveDraft(true)">{{
            $t("ai_capture.review.save_later")
          }}</Button>
          <Button
            :disabled="saving || submitting || reanalysisActive || reviewItems.length === 0"
            @click="
              submitReviewItems(
                selectedReviewItemIds.length
                  ? selectedReviewItemIds
                  : reviewItems.map(({ item: reviewItem }) => reviewItem.clientId)
              )
            "
          >
            <MdiLoading v-if="submitting" class="mr-2 animate-spin" /><MdiCheckCircle v-else class="mr-2" />
            {{
              $t("ai_capture.review.submit", {
                count: selectedReviewItemIds.length || reviewItems.length,
              })
            }}
          </Button>
        </div>
      </div>
    </div>

    <Card v-else-if="view === 'done' && activeSession">
      <CardContent class="py-10 text-center">
        <MdiCheckCircle class="mx-auto size-16 text-green-600" />
        <h2 class="mt-4 text-2xl font-bold">
          {{ $t("ai_capture.done.title") }}
        </h2>
        <p class="mt-2 text-muted-foreground">
          {{
            $t("ai_capture.done.description", {
              count: activeSession.createdItems.length,
              location: activeSession.location?.name,
            })
          }}
        </p>
        <div
          v-if="activeSession.createdItems.length"
          class="mx-auto mt-5 max-w-md divide-y rounded-lg border text-left"
        >
          <NuxtLink
            v-for="item in activeSession.createdItems"
            :key="item.id"
            :to="`/item/${item.id}`"
            class="block px-4 py-3 hover:bg-muted"
            >{{ item.name }}</NuxtLink
          >
        </div>
        <div class="mt-6 flex flex-wrap justify-center gap-2">
          <Button variant="outline" @click="navigateTo(`/location/${activeSession.location?.id}`)">{{
            $t("ai_capture.done.view_location")
          }}</Button>
          <Button @click="newSession">{{ $t("ai_capture.done.capture_more") }}</Button>
        </div>
      </CardContent>
    </Card>
  </BaseContainer>
</template>
