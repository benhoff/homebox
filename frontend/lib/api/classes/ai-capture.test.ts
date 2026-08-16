import { afterEach, describe, expect, test, vi } from "vitest";
import { Requests } from "../../requests";
import { AICaptureAPI, type AICaptureDraft } from "./ai-capture";

describe("AICaptureAPI", () => {
  afterEach(() => vi.restoreAllMocks());

  test("sends multiple photos and an editable correction draft as multipart form data", async () => {
    const responseDraft: AICaptureDraft = {
      items: [
        {
          clientId: "item-1",
          name: "Drill",
          quantity: 1,
          description: "",
          manufacturer: "",
          modelNumber: "",
          entityTypeId: "type-1",
          tagIds: [],
          photoIndexes: [0, 1],
          needsReview: false,
        },
      ],
      warnings: [],
    };
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify(responseDraft), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      })
    );
    const api = new AICaptureAPI(new Requests("http://homebox.test", "Bearer token"));
    const photos = [
      new File(["one"], "one.jpg", { type: "image/jpeg" }),
      new File(["two"], "two.jpg", { type: "image/jpeg" }),
    ];

    const result = await api.analyze(
      photos,
      { id: "location-1", name: "Garage" },
      {
        draft: responseDraft,
        instruction: "Make the quantity two",
      }
    );

    expect(result.error).toBe(false);
    expect(fetchMock).toHaveBeenCalledOnce();
    const [url, init] = fetchMock.mock.calls[0]!;
    expect(url).toBe("http://homebox.test/api/v1/ai/capture/analyze");
    expect(init?.headers).toMatchObject({ Authorization: "Bearer token" });
    const form = init?.body as FormData;
    expect(form.getAll("photos")).toHaveLength(2);
    expect(form.get("locationId")).toBe("location-1");
    expect(form.get("instruction")).toBe("Make the quantity two");
    expect(JSON.parse(form.get("draft") as string)).toEqual(responseDraft);
  });

  test("creates a durable session and uploads each photo with its idempotency id", async () => {
    const session = {
      id: "session-1",
      status: "capturing",
      photoCount: 0,
      uploadedPhotoCount: 0,
      photos: [],
      analysisAttempts: 0,
      draftRevision: 0,
      captureRevision: 0,
      createdItems: [],
      createdAt: "2026-08-16T00:00:00Z",
      updatedAt: "2026-08-16T00:00:00Z",
      expiresAt: "2026-09-15T00:00:00Z",
    };
    const photo = {
      id: "photo-1",
      clientPhotoId: "43fe45a5-c245-45a3-865d-7c89d50dcd6c",
      position: 0,
      captureGroupId: "b3d0d6b3-bda4-4c26-8d27-55f1d4653de3",
      originalName: "capture.jpg",
      mimeType: "image/jpeg",
      sizeBytes: 5,
      createdAt: "2026-08-16T00:00:00Z",
    };
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(
        new Response(JSON.stringify(session), {
          status: 201,
          headers: { "Content-Type": "application/json" },
        })
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify(photo), {
          status: 201,
          headers: { "Content-Type": "application/json" },
        })
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ ...session, status: "queued", photoCount: 1 }), {
          status: 202,
          headers: { "Content-Type": "application/json" },
        })
      );
    const api = new AICaptureAPI(new Requests("http://homebox.test", "Bearer token"));

    await api.createSession("location-1");
    await api.uploadSessionPhoto(
      "session-1",
      photo.clientPhotoId,
      0,
      new File(["image"], "capture.jpg", { type: "image/jpeg" }),
      "capture.jpg",
      photo.captureGroupId
    );
    await api.finishSession("session-1", 1, 1);

    expect(fetchMock.mock.calls[0]?.[0]).toBe("http://homebox.test/api/v1/ai/capture/sessions");
    expect(JSON.parse(fetchMock.mock.calls[0]?.[1]?.body as string)).toEqual({
      locationId: "location-1",
    });
    const upload = fetchMock.mock.calls[1]?.[1]?.body as FormData;
    expect(upload.get("clientPhotoId")).toBe(photo.clientPhotoId);
    expect(upload.get("position")).toBe("0");
    expect(upload.get("captureGroupId")).toBe(photo.captureGroupId);
    expect(upload.get("file")).toBeInstanceOf(File);
    expect(JSON.parse(fetchMock.mock.calls[2]?.[1]?.body as string)).toEqual({
      expectedPhotoCount: 1,
      expectedCaptureRevision: 1,
    });
  });

  test("reassigns and clears an uploaded photo group explicitly", async () => {
    const photo = {
      id: "photo-1",
      clientPhotoId: "43fe45a5-c245-45a3-865d-7c89d50dcd6c",
      position: 0,
      captureGroupId: null,
      originalName: "capture.jpg",
      mimeType: "image/jpeg",
      sizeBytes: 5,
      createdAt: "2026-08-16T00:00:00Z",
    };
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify(photo), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      })
    );
    const api = new AICaptureAPI(new Requests("http://homebox.test", "Bearer token"));

    await api.updateSessionPhotoGroup("session-1", "photo-1", null);

    expect(fetchMock.mock.calls[0]?.[0]).toBe(
      "http://homebox.test/api/v1/ai/capture/sessions/session-1/photos/photo-1"
    );
    expect(fetchMock.mock.calls[0]?.[1]?.method).toBe("PATCH");
    expect(JSON.parse(fetchMock.mock.calls[0]?.[1]?.body as string)).toEqual({
      captureGroupId: null,
    });
  });

  test("submits only the selected reviewed items", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ id: "session-1", status: "ready_for_review" }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      })
    );
    const api = new AICaptureAPI(new Requests("http://homebox.test", "Bearer token"));

    await api.submitItems("session-1", 4, ["item-2", "item-4"]);

    expect(fetchMock).toHaveBeenCalledOnce();
    expect(fetchMock.mock.calls[0]?.[0]).toBe("http://homebox.test/api/v1/ai/capture/sessions/session-1/submit-items");
    expect(JSON.parse(fetchMock.mock.calls[0]?.[1]?.body as string)).toEqual({
      revision: 4,
      clientIds: ["item-2", "item-4"],
    });
  });
});
