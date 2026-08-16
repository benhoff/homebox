import { BaseAPI, route } from "../base";
import type { Requests } from "../../requests";

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
  needsReview: boolean;
  reviewReason?: string;
}

export interface AICaptureDraft {
  items: AICaptureItem[];
  warnings: string[];
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
  originalName: string;
  mimeType: string;
  sizeBytes: number;
  createdAt: string;
}

export interface AICaptureCreatedItem {
  id: string;
  name: string;
}

export interface AICaptureSession {
  id: string;
  status: AICaptureSessionStatus;
  location?: AICaptureLocation;
  photoCount: number;
  uploadedPhotoCount: number;
  photos: AICaptureSessionPhoto[];
  analysisAttempts: number;
  draftRevision: number;
  draft?: AICaptureDraft;
  createdItems: AICaptureCreatedItem[];
  errorCode?: string;
  errorMessage?: string;
  createdAt: string;
  updatedAt: string;
  finishedAt?: string;
  analyzedAt?: string;
  completedAt?: string;
  expiresAt: string;
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

  uploadSessionPhoto(sessionId: string, clientPhotoId: string, position: number, file: File | Blob, name: string) {
    const formData = new FormData();
    formData.append("file", file, name);
    formData.append("clientPhotoId", clientPhotoId);
    formData.append("position", position.toString());
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

  finishSession(sessionId: string, expectedPhotoCount: number) {
    return this.http.post<{ expectedPhotoCount: number }, AICaptureSession>({
      url: route(`/ai/capture/sessions/${sessionId}/finish`),
      body: { expectedPhotoCount },
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

  submitSession(sessionId: string, revision: number) {
    return this.http.post<{ revision: number }, AICaptureSession>({
      url: route(`/ai/capture/sessions/${sessionId}/submit`),
      body: { revision },
    });
  }
}
