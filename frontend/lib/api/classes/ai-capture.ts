import { BaseAPI, route } from "../base";
import type { Requests } from "../../requests";

export type AICaptureMoveDisposition = "undecided" | "keep" | "sell" | "give_away" | "donate" | "recycle" | "trash";

export interface AICaptureItem {
  clientId: string;
  name: string;
  quantity: number;
  description: string;
  manufacturer: string;
  modelNumber: string;
  entityTypeId: string;
  tagIds: string[];
  photoIndexes: number[];
  photoIds?: string[];
  captureGroupId?: string;
  moveDisposition: AICaptureMoveDisposition;
  moveDispositionNote?: string;
  needsReview: boolean;
  reviewReason?: string;
}

export interface AICaptureDraft {
  items: AICaptureItem[];
  warnings: string[];
}

export interface AICaptureReanalysis {
  item: AICaptureItem;
  provider: string;
  warnings: string[];
}

export interface AIItemReanalysis extends AICaptureReanalysis {
  photoCount: number;
}

export type AICaptureReanalysisStatus = "queued" | "processing" | "waiting" | "completed";

export interface AICaptureReanalysisBatch {
  status: AICaptureReanalysisStatus;
  provider: string;
  total: number;
  completed: number;
  attempts: number;
  clientIds: string[];
  pendingClientIds: string[];
  suggestions: Record<string, AICaptureReanalysis>;
  errors: Record<string, string>;
  nextAttemptAt?: string;
  createdAt: string;
  updatedAt: string;
  completedAt?: string;
}

export interface AICaptureLocation {
  id: string;
  name: string;
}

export type AICaptureSessionStatus =
  "capturing" | "queued" | "analyzing" | "analysis_failed" | "ready_for_review" | "submitting" | "completed";

export interface AICaptureSessionPhoto {
  id: string;
  clientPhotoId: string;
  position: number;
  captureGroupId: string | null;
  originalName: string;
  mimeType: string;
  sizeBytes: number;
  createdAt: string;
}

export interface AICaptureCreatedItem {
  id: string;
  clientId: string;
  name: string;
}

export interface AICaptureItemSubmission {
  clientId: string;
  status: "pending" | "creating" | "attaching" | "completed" | "failed";
  entityId?: string;
  errorCode?: string;
}

export interface AICaptureSession {
  id: string;
  status: AICaptureSessionStatus;
  location?: AICaptureLocation;
  photoCount: number;
  uploadedPhotoCount: number;
  photos: AICaptureSessionPhoto[];
  analysisAttempts: number;
  analysisNextAttemptAt?: string;
  draftRevision: number;
  captureRevision: number;
  draft?: AICaptureDraft;
  createdItems: AICaptureCreatedItem[];
  submissions: AICaptureItemSubmission[];
  errorCode?: string;
  errorMessage?: string;
  createdAt: string;
  updatedAt: string;
  finishedAt?: string;
  analyzedAt?: string;
  completedAt?: string;
  expiresAt: string;
  reanalysis?: AICaptureReanalysisBatch;
}

export class AICaptureAPI extends BaseAPI {
  constructor(http: Requests, attachmentToken = "") {
    super(http, attachmentToken);
  }

  analyze(photos: File[], location: AICaptureLocation, options: { draft?: AICaptureDraft; instruction?: string } = {}) {
    const formData = new FormData();
    for (const photo of photos) {
      formData.append("photos", photo, photo.name);
    }
    formData.append("locationId", location.id);
    if (options.draft) {
      formData.append("draft", JSON.stringify(options.draft));
    }
    if (options.instruction?.trim()) {
      formData.append("instruction", options.instruction.trim());
    }

    return this.http.post<FormData, AICaptureDraft>({
      url: route("/ai/capture/analyze"),
      data: formData,
    });
  }

  createSession(locationId: string) {
    return this.http.post<{ locationId: string }, AICaptureSession>({
      url: route("/ai/capture/sessions"),
      body: { locationId },
    });
  }

  listSessions() {
    return this.http.get<{ items: AICaptureSession[] }>({
      url: route("/ai/capture/sessions"),
    });
  }

  getSession(sessionId: string) {
    return this.http.get<AICaptureSession>({
      url: route(`/ai/capture/sessions/${sessionId}`),
    });
  }

  updateSessionLocation(sessionId: string, locationId: string) {
    return this.http.patch<{ locationId: string }, AICaptureSession>({
      url: route(`/ai/capture/sessions/${sessionId}`),
      body: { locationId },
    });
  }

  deleteSession(sessionId: string) {
    return this.http.delete<void>({
      url: route(`/ai/capture/sessions/${sessionId}`),
    });
  }

  uploadSessionPhoto(
    sessionId: string,
    clientPhotoId: string,
    position: number,
    file: File | Blob,
    name: string,
    captureGroupId?: string
  ) {
    const formData = new FormData();
    formData.append("file", file, name);
    formData.append("clientPhotoId", clientPhotoId);
    formData.append("position", position.toString());
    if (captureGroupId) formData.append("captureGroupId", captureGroupId);
    return this.http.post<FormData, AICaptureSessionPhoto>({
      url: route(`/ai/capture/sessions/${sessionId}/photos`),
      data: formData,
    });
  }

  photoURL(sessionId: string, photoId: string) {
    const url = `/ai/capture/sessions/${sessionId}/photos/${photoId}`;
    return this.attachmentToken ? this.authURL(url) : route(url);
  }

  deleteSessionPhoto(sessionId: string, photoId: string) {
    return this.http.delete<void>({
      url: route(`/ai/capture/sessions/${sessionId}/photos/${photoId}`),
    });
  }

  updateSessionPhotoGroup(sessionId: string, photoId: string, captureGroupId: string | null) {
    return this.http.patch<{ captureGroupId: string | null }, AICaptureSessionPhoto>({
      url: route(`/ai/capture/sessions/${sessionId}/photos/${photoId}`),
      body: { captureGroupId },
    });
  }

  finishSession(sessionId: string, expectedPhotoCount: number, expectedCaptureRevision: number) {
    return this.http.post<{ expectedPhotoCount: number; expectedCaptureRevision: number }, AICaptureSession>({
      url: route(`/ai/capture/sessions/${sessionId}/finish`),
      body: { expectedPhotoCount, expectedCaptureRevision },
    });
  }

  retryAnalysis(sessionId: string) {
    return this.http.post<Record<string, never>, AICaptureSession>({
      url: route(`/ai/capture/sessions/${sessionId}/retry-analysis`),
      body: {},
    });
  }

  saveDraft(sessionId: string, revision: number, draft: AICaptureDraft) {
    return this.http.put<{ revision: number; draft: AICaptureDraft }, AICaptureSession>({
      url: route(`/ai/capture/sessions/${sessionId}/draft`),
      body: { revision, draft },
    });
  }

  correctSession(sessionId: string, revision: number, instruction: string) {
    return this.http.post<{ revision: number; instruction: string }, AICaptureSession>({
      url: route(`/ai/capture/sessions/${sessionId}/corrections`),
      body: { revision, instruction },
    });
  }

  reanalyzeItem(sessionId: string, revision: number, clientId: string, provider: string, instruction = "") {
    return this.http.post<
      {
        revision: number;
        clientId: string;
        provider: string;
        instruction: string;
      },
      AICaptureReanalysis
    >({
      url: route(`/ai/capture/sessions/${sessionId}/reanalyze-item`),
      body: { revision, clientId, provider, instruction },
    });
  }

  reanalyzeInventoryItem(itemId: string, instruction = "") {
    return this.http.post<{ instruction: string }, AIItemReanalysis>({
      url: route(`/ai/items/${itemId}/reanalyze`),
      body: { instruction: instruction.trim() },
    });
  }

  queueReanalysis(sessionId: string, revision: number, clientIds: string[], provider: string, instruction = "") {
    return this.http.post<
      {
        revision: number;
        clientIds: string[];
        provider: string;
        instruction: string;
      },
      AICaptureSession
    >({
      url: route(`/ai/capture/sessions/${sessionId}/reanalyze-items`),
      body: { revision, clientIds, provider, instruction },
    });
  }

  dismissReanalysis(sessionId: string, clientId: string) {
    return this.http.delete<AICaptureSession>({
      url: route(`/ai/capture/sessions/${sessionId}/reanalysis/${encodeURIComponent(clientId)}`),
    });
  }

  submitSession(sessionId: string, revision: number) {
    return this.http.post<{ revision: number }, AICaptureSession>({
      url: route(`/ai/capture/sessions/${sessionId}/submit`),
      body: { revision },
    });
  }

  submitItems(sessionId: string, revision: number, clientIds: string[]) {
    return this.http.post<{ revision: number; clientIds: string[] }, AICaptureSession>({
      url: route(`/ai/capture/sessions/${sessionId}/submit-items`),
      body: { revision, clientIds },
    });
  }
}
