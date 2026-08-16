import { reactive } from "vue";
import { afterEach, describe, expect, test, vi } from "vitest";
import { putCaptureQueuePhoto, type CaptureQueuePhoto } from "./ai-capture-queue";

function installIndexedDBHarness() {
  let storedPhoto: CaptureQueuePhoto | undefined;
  const database = {
    close: vi.fn(),
    transaction: () => {
      const transaction = {
        error: null,
        objectStore: () =>
          ({
            put: (value: CaptureQueuePhoto) => {
              storedPhoto = structuredClone(value);
              const request = { result: undefined, error: null } as unknown as IDBRequest;
              queueMicrotask(() => {
                request.onsuccess?.call(request, new Event("success"));
                transaction.oncomplete?.call(transaction, new Event("complete"));
              });
              return request;
            },
          }) as unknown as IDBObjectStore,
      } as unknown as IDBTransaction;
      return transaction;
    },
  } as unknown as IDBDatabase;
  const factory = {
    open: () => {
      const request = { result: database, error: null } as unknown as IDBOpenDBRequest;
      queueMicrotask(() => request.onsuccess?.call(request, new Event("success")));
      return request;
    },
  } as unknown as IDBFactory;
  vi.stubGlobal("indexedDB", factory);
  return () => storedPhoto;
}

describe("AI capture queue", () => {
  afterEach(() => vi.unstubAllGlobals());

  test("persists Vue-reactive photos as cloneable IndexedDB records", async () => {
    const storedPhoto = installIndexedDBHarness();
    const photo = reactive<CaptureQueuePhoto>({
      key: "session-1:photo-1",
      sessionId: "session-1",
      clientPhotoId: "photo-1",
      position: 0,
      captureGroupId: null,
      name: "capture.jpg",
      blob: new Blob(["image"], { type: "image/jpeg" }),
      status: "uploading",
      attempts: 1,
      createdAt: 1,
    });

    expect(() => structuredClone(photo)).toThrowError(/could not be cloned/i);

    await putCaptureQueuePhoto(photo);

    expect(storedPhoto()).toMatchObject({
      key: photo.key,
      status: "uploading",
      attempts: 1,
    });
    expect(storedPhoto()?.blob).toBeInstanceOf(Blob);
  });
});
