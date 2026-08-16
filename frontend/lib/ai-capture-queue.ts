export type CaptureQueueStatus = "pending" | "uploading" | "failed";

export interface CaptureQueuePhoto {
  key: string;
  sessionId: string;
  clientPhotoId: string;
  position: number;
  captureGroupId: string | null;
  name: string;
  blob: Blob;
  status: CaptureQueueStatus;
  attempts: number;
  error?: string;
  createdAt: number;
}

export interface CaptureQueueSessionState {
  sessionId: string;
  sameItemMode: boolean;
  activeCaptureGroupId?: string;
  updatedAt: number;
}

const databaseName = "homebox-ai-capture";
const photoStoreName = "photos";
const stateStoreName = "session-state";
const version = 2;

function openCaptureDatabase(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(databaseName, version);
    request.onupgradeneeded = () => {
      const database = request.result;
      if (!database.objectStoreNames.contains(photoStoreName)) {
        const store = database.createObjectStore(photoStoreName, {
          keyPath: "key",
        });
        store.createIndex("sessionId", "sessionId", { unique: false });
      }
      if (!database.objectStoreNames.contains(stateStoreName)) {
        database.createObjectStore(stateStoreName, { keyPath: "sessionId" });
      }
    };
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error || new Error("Could not open the capture queue"));
  });
}

function transactionRequest<T>(
  objectStoreName: string,
  mode: IDBTransactionMode,
  operation: (store: IDBObjectStore) => IDBRequest<T>
): Promise<T> {
  return openCaptureDatabase().then(
    database =>
      new Promise<T>((resolve, reject) => {
        const transaction = database.transaction(objectStoreName, mode);
        const request = operation(transaction.objectStore(objectStoreName));
        let result: T;
        request.onsuccess = () => {
          result = request.result;
        };
        request.onerror = () => reject(request.error || new Error("Capture queue operation failed"));
        transaction.oncomplete = () => {
          database.close();
          resolve(result);
        };
        transaction.onerror = () => reject(transaction.error || new Error("Capture queue transaction failed"));
      })
  );
}

export function captureQueueKey(sessionId: string, clientPhotoId: string) {
  return `${sessionId}:${clientPhotoId}`;
}

export async function putCaptureQueuePhoto(photo: CaptureQueuePhoto) {
  await transactionRequest(photoStoreName, "readwrite", store => store.put(photo));
}

export async function putCaptureQueuePhotoWithState(photo: CaptureQueuePhoto, state: CaptureQueueSessionState) {
  const database = await openCaptureDatabase();
  await new Promise<void>((resolve, reject) => {
    const transaction = database.transaction([photoStoreName, stateStoreName], "readwrite");
    transaction.objectStore(photoStoreName).put(photo);
    transaction.objectStore(stateStoreName).put(state);
    transaction.oncomplete = () => {
      database.close();
      resolve();
    };
    transaction.onerror = () => reject(transaction.error || new Error("Could not save the captured photo"));
  });
}

export async function deleteCaptureQueuePhoto(sessionId: string, clientPhotoId: string) {
  await transactionRequest(photoStoreName, "readwrite", store =>
    store.delete(captureQueueKey(sessionId, clientPhotoId))
  );
}

export async function listCaptureQueuePhotos(sessionId: string): Promise<CaptureQueuePhoto[]> {
  const database = await openCaptureDatabase();
  return await new Promise((resolve, reject) => {
    const transaction = database.transaction(photoStoreName, "readonly");
    const request = transaction.objectStore(photoStoreName).index("sessionId").getAll(sessionId);
    request.onsuccess = () =>
      resolve((request.result as CaptureQueuePhoto[]).sort((left, right) => left.position - right.position));
    request.onerror = () => reject(request.error || new Error("Could not restore captured photos"));
    transaction.oncomplete = () => database.close();
  });
}

export async function getCaptureQueueSessionState(sessionId: string): Promise<CaptureQueueSessionState | undefined> {
  return await transactionRequest(stateStoreName, "readonly", store => store.get(sessionId));
}

export async function putCaptureQueueSessionState(state: CaptureQueueSessionState) {
  await transactionRequest(stateStoreName, "readwrite", store => store.put(state));
}

export async function deleteCaptureQueueSessionState(sessionId: string) {
  await transactionRequest(stateStoreName, "readwrite", store => store.delete(sessionId));
}

export async function clearCaptureQueueSession(sessionId: string) {
  const photos = await listCaptureQueuePhotos(sessionId);
  await Promise.all(photos.map(photo => deleteCaptureQueuePhoto(sessionId, photo.clientPhotoId)));
  await deleteCaptureQueueSessionState(sessionId);
}

export async function normalizeCaptureImage(file: File | Blob, name = "capture.jpg"): Promise<File> {
  const source = await loadCaptureImage(file);
  const maxDimension = 2048;
  const scale = Math.min(1, maxDimension / Math.max(source.width, source.height));
  const canvas = document.createElement("canvas");
  canvas.width = Math.max(1, Math.round(source.width * scale));
  canvas.height = Math.max(1, Math.round(source.height * scale));
  const context = canvas.getContext("2d");
  if (!context) {
    source.close?.();
    throw new Error("Canvas is unavailable");
  }
  context.drawImage(source, 0, 0, canvas.width, canvas.height);
  source.close?.();
  const blob = await new Promise<Blob>((resolve, reject) => {
    canvas.toBlob(
      value => (value ? resolve(value) : reject(new Error("Could not prepare the captured photo"))),
      "image/jpeg",
      0.9
    );
  });
  canvas.width = 0;
  canvas.height = 0;
  return new File([blob], name.replace(/\.[^.]+$/, "") + ".jpg", {
    type: "image/jpeg",
  });
}

type CaptureImageSource = CanvasImageSource & {
  width: number;
  height: number;
  close?: () => void;
};

async function loadCaptureImage(file: File | Blob): Promise<CaptureImageSource> {
  if (typeof createImageBitmap === "function") {
    return (await createImageBitmap(file, {
      imageOrientation: "from-image",
    })) as CaptureImageSource;
  }

  const url = URL.createObjectURL(file);
  try {
    const image = new Image();
    image.src = url;
    await new Promise<void>((resolve, reject) => {
      image.onload = () => resolve();
      image.onerror = () => reject(new Error("Could not read the captured photo"));
    });
    return Object.assign(image, {
      width: image.naturalWidth,
      height: image.naturalHeight,
      close: () => URL.revokeObjectURL(url),
    }) as CaptureImageSource;
  } catch (error) {
    URL.revokeObjectURL(url);
    throw error;
  }
}
