import { BaseAPI, route } from "../base";

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
  needsReview: boolean;
  reviewReason?: string;
}

export interface AICaptureDraft {
  items: AICaptureItem[];
  warnings: string[];
}

export class AICaptureAPI extends BaseAPI {
  analyze(photos: File[], options: { draft?: AICaptureDraft; instruction?: string } = {}) {
    const formData = new FormData();
    for (const photo of photos) {
      formData.append("photos", photo, photo.name);
    }
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
}
