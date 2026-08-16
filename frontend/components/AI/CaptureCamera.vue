<script setup lang="ts">
  import { Button } from "~/components/ui/button";
  import { Switch } from "~/components/ui/switch";
  import MdiCamera from "~icons/mdi/camera";
  import MdiCameraFlip from "~icons/mdi/camera-flip";
  import MdiFlash from "~icons/mdi/flash";
  import MdiFlashOff from "~icons/mdi/flash-off";
  import MdiImageMultiple from "~icons/mdi/image-multiple";
  import MdiLoading from "~icons/mdi/loading";

  const props = defineProps<{
    count: number;
    limitReached?: boolean;
    busy?: boolean;
    sameItemMode: boolean;
    activeGroupNumber?: number;
    activeGroupViewCount: number;
  }>();

  const emit = defineEmits<{
    captured: [file: File];
    finish: [];
    nextItem: [];
    "update:sameItemMode": [value: boolean];
  }>();

  const video = ref<HTMLVideoElement>();
  const canvas = ref<HTMLCanvasElement>();
  const picker = ref<HTMLInputElement>();
  const stream = shallowRef<MediaStream>();
  const cameraError = ref("");
  const starting = ref(false);
  const facingMode = ref<"environment" | "user">("environment");
  const torchAvailable = ref(false);
  const torchEnabled = ref(false);
  const full = computed(() => props.limitReached === true);

  function stopCamera() {
    stream.value?.getTracks().forEach(track => track.stop());
    stream.value = undefined;
    torchAvailable.value = false;
    torchEnabled.value = false;
    if (video.value) video.value.srcObject = null;
  }

  async function startCamera() {
    stopCamera();
    cameraError.value = "";
    starting.value = true;
    try {
      if (!navigator.mediaDevices?.getUserMedia) throw new Error("unsupported");
      const next = await navigator.mediaDevices.getUserMedia({
        audio: false,
        video: {
          facingMode: { ideal: facingMode.value },
          width: { ideal: 1920 },
          height: { ideal: 1080 },
        },
      });
      stream.value = next;
      if (video.value) {
        video.value.srcObject = next;
        await video.value.play();
      }
      const track = next.getVideoTracks()[0];
      const capabilities = track?.getCapabilities?.() as (MediaTrackCapabilities & { torch?: boolean }) | undefined;
      torchAvailable.value = capabilities?.torch === true;
    } catch (error) {
      console.error(error);
      cameraError.value =
        !window.isSecureContext && location.hostname !== "localhost"
          ? "ai_capture.camera.https_required"
          : "ai_capture.camera.unavailable";
    } finally {
      starting.value = false;
    }
  }

  async function switchCamera() {
    facingMode.value = facingMode.value === "environment" ? "user" : "environment";
    await startCamera();
  }

  async function toggleTorch() {
    const track = stream.value?.getVideoTracks()[0];
    if (!track || !torchAvailable.value) return;
    try {
      torchEnabled.value = !torchEnabled.value;
      await track.applyConstraints({
        advanced: [{ torch: torchEnabled.value } as MediaTrackConstraintSet],
      });
    } catch (error) {
      console.error(error);
      torchEnabled.value = false;
    }
  }

  async function takePhoto() {
    if (full.value || props.busy || !video.value || !canvas.value || video.value.readyState < 2) return;
    const target = canvas.value;
    target.width = video.value.videoWidth;
    target.height = video.value.videoHeight;
    const context = target.getContext("2d");
    if (!context) return;
    if (facingMode.value === "user") {
      context.translate(target.width, 0);
      context.scale(-1, 1);
    }
    context.drawImage(video.value, 0, 0, target.width, target.height);
    const blob = await new Promise<Blob | null>(resolve => target.toBlob(resolve, "image/jpeg", 0.92));
    target.width = 0;
    target.height = 0;
    if (!blob) return;
    emit("captured", new File([blob], `capture-${Date.now()}.jpg`, { type: "image/jpeg" }));
  }

  function selectFiles(event: Event) {
    const input = event.target as HTMLInputElement;
    for (const file of Array.from(input.files || [])) emit("captured", file);
    input.value = "";
  }

  function handleVisibility() {
    if (document.hidden) stopCamera();
    else void startCamera();
  }

  onMounted(() => {
    void startCamera();
    document.addEventListener("visibilitychange", handleVisibility);
  });
  onBeforeUnmount(() => {
    document.removeEventListener("visibilitychange", handleVisibility);
    stopCamera();
  });
</script>

<template>
  <div class="relative flex min-h-[65dvh] flex-col overflow-hidden rounded-xl bg-black text-white">
    <div class="relative min-h-0 grow">
      <video
        v-show="stream"
        ref="video"
        autoplay
        muted
        playsinline
        class="absolute inset-0 size-full object-cover"
        :class="facingMode === 'user' ? '-scale-x-100' : ''"
      />
      <div v-if="starting" class="absolute inset-0 grid place-items-center">
        <MdiLoading class="size-10 animate-spin" />
      </div>
      <div v-else-if="cameraError" class="absolute inset-0 grid place-items-center p-8 text-center">
        <div>
          <MdiCamera class="mx-auto mb-3 size-12 text-white/70" />
          <p class="font-medium">{{ $t(cameraError) }}</p>
          <p class="mt-2 text-sm text-white/70">
            {{ $t("ai_capture.camera.fallback_help") }}
          </p>
          <Button class="mt-4" variant="secondary" @click="picker?.click()">
            <MdiImageMultiple class="mr-2" />
            {{ $t("ai_capture.camera.choose_photos") }}
          </Button>
        </div>
      </div>

      <div class="absolute left-3 top-3 rounded-full bg-black/60 px-3 py-1 text-sm backdrop-blur">
        {{ $t("ai_capture.camera.photo_count", { count }) }}
      </div>
      <div v-if="stream" class="absolute right-3 top-3 flex gap-2">
        <Button v-if="torchAvailable" size="icon" variant="secondary" class="rounded-full" @click="toggleTorch">
          <MdiFlashOff v-if="torchEnabled" />
          <MdiFlash v-else />
          <span class="sr-only">{{ $t("ai_capture.camera.flash") }}</span>
        </Button>
        <Button size="icon" variant="secondary" class="rounded-full" @click="switchCamera">
          <MdiCameraFlip />
          <span class="sr-only">{{ $t("ai_capture.camera.switch") }}</span>
        </Button>
      </div>
    </div>

    <div class="flex flex-wrap items-center justify-between gap-3 border-b border-white/15 bg-black/90 px-4 py-3">
      <div class="flex items-center gap-3">
        <Switch
          id="same-item-mode"
          :model-value="sameItemMode"
          :disabled="busy || full"
          @update:model-value="emit('update:sameItemMode', $event)"
        />
        <label for="same-item-mode" class="cursor-pointer text-sm font-medium">
          {{ $t("ai_capture.camera.same_item_mode") }}
        </label>
      </div>
      <div v-if="sameItemMode" class="flex items-center gap-3">
        <span class="text-sm text-white/75">
          {{
            activeGroupNumber
              ? $t("ai_capture.camera.group_views", {
                  group: activeGroupNumber,
                  count: activeGroupViewCount,
                })
              : $t("ai_capture.camera.group_ready")
          }}
        </span>
        <Button
          size="sm"
          variant="secondary"
          :disabled="busy || !activeGroupNumber || activeGroupViewCount === 0"
          :aria-describedby="activeGroupNumber ? 'same-item-group-status' : undefined"
          @click="emit('nextItem')"
        >
          {{ $t("ai_capture.camera.next_item") }}
        </Button>
      </div>
      <p id="same-item-group-status" class="sr-only" aria-live="polite">
        <template v-if="sameItemMode && activeGroupNumber">
          {{
            $t("ai_capture.camera.group_views", {
              group: activeGroupNumber,
              count: activeGroupViewCount,
            })
          }}
        </template>
      </p>
    </div>

    <div class="grid grid-cols-[1fr_auto_1fr] items-center gap-4 bg-black/90 px-4 py-5">
      <Button variant="ghost" class="justify-self-start text-white hover:text-white" @click="picker?.click()">
        <MdiImageMultiple class="mr-2" />
        <span class="hidden sm:inline">{{ $t("ai_capture.camera.upload") }}</span>
      </Button>
      <button
        type="button"
        class="grid size-20 place-items-center rounded-full border-4 border-white bg-white/30 transition active:scale-95 disabled:opacity-40"
        :disabled="full || busy || !stream"
        :aria-label="$t('ai_capture.camera.shutter')"
        @click="takePhoto"
      >
        <span class="size-14 rounded-full bg-white" />
      </button>
      <Button class="justify-self-end" :disabled="count === 0 || busy" @click="emit('finish')">
        {{ $t("ai_capture.camera.finish") }}
      </Button>
    </div>
    <input ref="picker" class="sr-only" type="file" accept="image/*" multiple @change="selectFiles" />
    <canvas ref="canvas" class="hidden" />
  </div>
</template>
