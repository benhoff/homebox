<script setup lang="ts">
  import { toast } from "@/components/ui/sonner";
  import { useI18n } from "vue-i18n";
  import BaseContainer from "~/components/Base/Container.vue";
  import LocationCapturePicker from "~/components/Location/CapturePicker.vue";
  import PhotoUploader from "~/components/Form/PhotoUploader.vue";
  import TagSelector from "~/components/Tag/Selector.vue";
  import { Button } from "~/components/ui/button";
  import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "~/components/ui/card";
  import { Input } from "~/components/ui/input";
  import { Label } from "~/components/ui/label";
  import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "~/components/ui/select";
  import { Textarea } from "~/components/ui/textarea";
  import type { AICaptureDraft, AICaptureItem } from "~/lib/api/classes/ai-capture";
  import type { APISummary } from "~/lib/api/types/data-contracts";
  import { AttachmentTypes } from "~/lib/api/types/non-generated";
  import { deletePhoto, rotatePhotoPreview, type PhotoPreview } from "~/components/Form/photo-uploader";
  import MdiAlertCircleOutline from "~icons/mdi/alert-circle-outline";
  import MdiArrowLeft from "~icons/mdi/arrow-left";
  import MdiArrowRight from "~icons/mdi/arrow-right";
  import MdiCamera from "~icons/mdi/camera";
  import MdiCheckCircle from "~icons/mdi/check-circle";
  import MdiDelete from "~icons/mdi/delete";
  import MdiImageMultiple from "~icons/mdi/image-multiple";
  import MdiLoading from "~icons/mdi/loading";
  import MdiMagicStaff from "~icons/mdi/magic-staff";
  import MdiMapMarker from "~icons/mdi/map-marker";
  import MdiPackageVariant from "~icons/mdi/package-variant";
  import MdiRefresh from "~icons/mdi/refresh";
  import MdiRotateClockwise from "~icons/mdi/rotate-clockwise";
  import MdiUndo from "~icons/mdi/undo";

  definePageMeta({ middleware: ["auth"] });

  const { t } = useI18n();
  useHead({ title: `HomeBox | ${t("ai_capture.title")}` });

  type CaptureStep = "location" | "photos" | "review" | "done";
  type CaptureLocation = { id: string; name: string };
  type SubmitProgress = { id: string; uploaded: Set<number> };

  const api = useUserApi();
  const publicApi = usePublicApi();
  const entityTypeStore = useEntityTypeStore();
  const tagStore = useTagStore();
  const locationStore = useLocationStore();

  const step = ref<CaptureStep>("location");
  const selectedLocation = ref<CaptureLocation | null>(null);
  const photos = ref<PhotoPreview[]>([]);
  const draft = ref<AICaptureDraft | null>(null);
  const previousDraft = ref<AICaptureDraft | null>(null);
  const correction = ref("");
  const analyzing = ref(false);
  const submitting = ref(false);
  const status = ref<APISummary | null>(null);
  const submitFailures = ref<string[]>([]);
  const createdItems = ref<{ id: string; name: string }[]>([]);
  const submitProgress = new Map<string, SubmitProgress>();

  const itemTypes = computed(() => entityTypeStore.itemTypes);
  const tags = computed(() => tagStore.tags);
  const maxPhotos = computed(() => status.value?.ai?.maxPhotos || 8);
  const aiEnabled = computed(() => status.value?.ai?.enabled !== false);

  const stepNumber = computed(() => ({ location: 1, photos: 2, review: 3, done: 4 })[step.value]);
  const steps = computed(() => [
    { number: 1, label: t("ai_capture.steps.location") },
    { number: 2, label: t("ai_capture.steps.photos") },
    { number: 3, label: t("ai_capture.steps.review") },
    { number: 4, label: t("ai_capture.steps.done") },
  ]);

  function appendPhotos(nextPhotos: PhotoPreview[]) {
    const available = Math.max(0, maxPhotos.value - photos.value.length);
    if (nextPhotos.length > available) {
      toast.error(t("ai_capture.errors.too_many_photos", { count: maxPhotos.value }));
    }
    photos.value.push(...nextPhotos.slice(0, available));
  }

  function removePhoto(index: number) {
    photos.value = deletePhoto(photos.value, index);
  }

  async function rotatePhoto(index: number) {
    const photo = photos.value[index];
    if (!photo) return;
    try {
      photos.value[index] = await rotatePhotoPreview(photo);
    } catch (error) {
      console.error(error);
      toast.error(t("ai_capture.errors.rotate"));
    }
  }

  async function makeAnalysisFile(photo: PhotoPreview): Promise<File> {
    const image = new Image();
    image.src = photo.fileBase64;
    await new Promise<void>((resolve, reject) => {
      image.onload = () => resolve();
      image.onerror = () => reject(new Error(`Unable to read ${photo.photoName}`));
    });

    const maxDimension = 1600;
    const scale = Math.min(1, maxDimension / Math.max(image.naturalWidth, image.naturalHeight));
    const canvas = document.createElement("canvas");
    canvas.width = Math.max(1, Math.round(image.naturalWidth * scale));
    canvas.height = Math.max(1, Math.round(image.naturalHeight * scale));
    const context = canvas.getContext("2d");
    if (!context) throw new Error("Canvas is unavailable");
    context.drawImage(image, 0, 0, canvas.width, canvas.height);
    const blob = await new Promise<Blob>((resolve, reject) => {
      canvas.toBlob(
        value => (value ? resolve(value) : reject(new Error("Unable to prepare photo"))),
        "image/jpeg",
        0.85
      );
    });
    return new File([blob], `${photo.photoName.replace(/\.[^.]+$/, "") || "photo"}-analysis.jpg`, {
      type: "image/jpeg",
    });
  }

  async function runAnalysis(options: { correction?: boolean } = {}) {
    if (!selectedLocation.value) {
      toast.error(t("ai_capture.errors.location_required"));
      step.value = "location";
      return;
    }
    if (photos.value.length === 0) {
      toast.error(t("ai_capture.errors.photos_required"));
      step.value = "photos";
      return;
    }
    if (!aiEnabled.value) {
      toast.error(t("ai_capture.errors.disabled"));
      return;
    }
    if (options.correction && !correction.value.trim()) return;

    analyzing.value = true;
    const before = draft.value ? cloneDraft(draft.value) : null;
    try {
      const analysisFiles = await Promise.all(photos.value.map(makeAnalysisFile));
      const response = await api.aiCapture.analyze(analysisFiles, {
        draft: options.correction ? (draft.value ?? undefined) : undefined,
        instruction: options.correction ? correction.value : undefined,
      });
      if (response.error || !response.data?.items?.length) {
        toast.error(t("ai_capture.errors.analysis_failed"));
        return;
      }
      if (options.correction) previousDraft.value = before;
      draft.value = response.data;
      correction.value = "";
      step.value = "review";
      toast.success(options.correction ? t("ai_capture.correction_applied") : t("ai_capture.analysis_complete"));
    } catch (error) {
      console.error(error);
      toast.error(t("ai_capture.errors.analysis_failed"));
    } finally {
      analyzing.value = false;
    }
  }

  function cloneDraft(value: AICaptureDraft): AICaptureDraft {
    return JSON.parse(JSON.stringify(value)) as AICaptureDraft;
  }

  function undoCorrection() {
    if (!previousDraft.value) return;
    draft.value = previousDraft.value;
    previousDraft.value = null;
  }

  function addItem() {
    if (!draft.value) return;
    draft.value.items.push({
      clientId: `manual-${Date.now()}`,
      name: "",
      quantity: 1,
      description: "",
      manufacturer: "",
      modelNumber: "",
      entityTypeId: itemTypes.value[0]?.id ?? "",
      tagIds: [],
      photoIndexes: photos.value.map((_, index) => index),
      needsReview: true,
      reviewReason: t("ai_capture.manual_item"),
    });
  }

  function removeItem(index: number) {
    draft.value?.items.splice(index, 1);
  }

  function togglePhoto(item: AICaptureItem, photoIndex: number, checked: boolean) {
    item.photoIndexes = checked
      ? [...new Set([...item.photoIndexes, photoIndex])].sort((a, b) => a - b)
      : item.photoIndexes.filter(index => index !== photoIndex);
  }

  function validateDraft(): boolean {
    if (!draft.value?.items.length) {
      toast.error(t("ai_capture.errors.items_required"));
      return false;
    }
    for (const item of draft.value.items) {
      if (!item.name.trim() || !item.entityTypeId || item.quantity <= 0) {
        toast.error(t("ai_capture.errors.review_required"));
        return false;
      }
    }
    return true;
  }

  async function submitItems() {
    if (!selectedLocation.value || !validateDraft() || !draft.value) return;
    submitting.value = true;
    submitFailures.value = [];

    for (const item of draft.value.items) {
      let progress = submitProgress.get(item.clientId);
      try {
        if (!progress) {
          const created = await api.items.create({
            name: item.name.trim(),
            quantity: item.quantity,
            description: item.description.trim(),
            manufacturer: item.manufacturer.trim() || null,
            modelNumber: item.modelNumber.trim() || null,
            parentId: selectedLocation.value.id,
            entityTypeId: item.entityTypeId,
            tagIds: item.tagIds,
          });
          if (created.error || !created.data?.id) throw new Error(`Could not create ${item.name}`);
          progress = { id: created.data.id, uploaded: new Set<number>() };
          submitProgress.set(item.clientId, progress);
        }

        const assignedPhotos = [...new Set(item.photoIndexes)].filter(index => photos.value[index]);
        for (const [position, photoIndex] of assignedPhotos.entries()) {
          if (progress.uploaded.has(photoIndex)) continue;
          const photo = photos.value[photoIndex]!;
          const attachment = await api.items.attachments.add(
            progress.id,
            photo.file,
            photo.photoName,
            AttachmentTypes.Photo,
            position === 0
          );
          if (attachment.error) throw new Error(`Could not attach a photo to ${item.name}`);
          progress.uploaded.add(photoIndex);
        }

        if (!createdItems.value.some(created => created.id === progress!.id)) {
          createdItems.value.push({ id: progress.id, name: item.name });
        }
      } catch (error) {
        console.error(error);
        submitFailures.value.push(item.name);
      }
    }

    submitting.value = false;
    if (submitFailures.value.length > 0) {
      toast.error(t("ai_capture.errors.partial_submit"));
      return;
    }

    await locationStore.refreshChildren();
    step.value = "done";
    toast.success(t("ai_capture.submit_complete", { count: createdItems.value.length }));
  }

  function captureMore() {
    photos.value = [];
    draft.value = null;
    previousDraft.value = null;
    correction.value = "";
    submitFailures.value = [];
    createdItems.value = [];
    submitProgress.clear();
    step.value = selectedLocation.value ? "photos" : "location";
  }

  onMounted(async () => {
    await Promise.all([entityTypeStore.ensureFetched(), tagStore.ensureAllTagsFetched()]);
    const response = await publicApi.status();
    if (!response.error) status.value = response.data;
  });
</script>

<template>
  <BaseContainer class="max-w-4xl pb-24">
    <header class="mb-5">
      <div class="flex items-center gap-3">
        <div class="rounded-xl bg-primary p-3 text-primary-foreground">
          <MdiMagicStaff class="size-7" />
        </div>
        <div>
          <h1 class="text-2xl font-bold sm:text-3xl">
            {{ $t("ai_capture.title") }}
          </h1>
          <p class="text-sm text-muted-foreground">
            {{ $t("ai_capture.subtitle") }}
          </p>
        </div>
      </div>
    </header>

    <ol class="mb-5 grid grid-cols-4 gap-1" :aria-label="$t('ai_capture.progress')">
      <li v-for="item in steps" :key="item.number" class="min-w-0">
        <div class="flex items-center">
          <div
            class="flex size-8 shrink-0 items-center justify-center rounded-full border text-sm font-semibold"
            :class="item.number <= stepNumber ? 'border-primary bg-primary text-primary-foreground' : 'bg-background'"
          >
            <MdiCheckCircle v-if="item.number < stepNumber" class="size-5" />
            <span v-else>{{ item.number }}</span>
          </div>
          <div
            v-if="item.number < 4"
            class="h-0.5 grow"
            :class="item.number < stepNumber ? 'bg-primary' : 'bg-border'"
          />
        </div>
        <p
          class="mt-1 truncate text-xs"
          :class="item.number === stepNumber ? 'font-medium text-primary' : 'text-muted-foreground'"
        >
          {{ item.label }}
        </p>
      </li>
    </ol>

    <div
      v-if="status && !status.ai.enabled"
      class="mb-4 flex gap-3 rounded-lg border border-destructive bg-destructive/10 p-4 text-sm"
    >
      <MdiAlertCircleOutline class="size-5 shrink-0 text-destructive" />
      <p>{{ $t("ai_capture.disabled_help") }}</p>
    </div>

    <Card v-if="step === 'location'">
      <CardHeader>
        <CardTitle class="flex items-center gap-2"><MdiMapMarker /> {{ $t("ai_capture.location.title") }}</CardTitle>
        <CardDescription>{{ $t("ai_capture.location.description") }}</CardDescription>
      </CardHeader>
      <CardContent class="space-y-4">
        <LocationCapturePicker v-model="selectedLocation" />
        <div class="flex justify-end">
          <Button :disabled="!selectedLocation" @click="step = 'photos'">
            {{ $t("ai_capture.continue") }} <MdiArrowRight class="ml-2" />
          </Button>
        </div>
      </CardContent>
    </Card>

    <Card v-else-if="step === 'photos'">
      <CardHeader>
        <CardTitle class="flex items-center gap-2"><MdiCamera /> {{ $t("ai_capture.photos.title") }}</CardTitle>
        <CardDescription>{{ $t("ai_capture.photos.description", { count: maxPhotos }) }}</CardDescription>
      </CardHeader>
      <CardContent class="space-y-4">
        <div class="grid gap-3 sm:grid-cols-2">
          <PhotoUploader
            :existing-count="photos.length"
            :multiple="false"
            capture="environment"
            :label="$t('ai_capture.photos.take_label')"
            :button-label="$t('ai_capture.photos.take')"
            @selected="appendPhotos"
          />
          <PhotoUploader
            :existing-count="photos.length"
            :label="$t('ai_capture.photos.upload_label')"
            :button-label="$t('ai_capture.photos.upload')"
            @selected="appendPhotos"
          />
        </div>

        <div v-if="photos.length" class="grid grid-cols-2 gap-3 sm:grid-cols-3">
          <div
            v-for="(photo, index) in photos"
            :key="`${photo.photoName}-${index}`"
            class="overflow-hidden rounded-lg border bg-muted/30"
          >
            <img :src="photo.fileBase64" :alt="photo.photoName" class="aspect-square w-full object-cover" />
            <div class="flex items-center gap-1 p-2">
              <span class="min-w-0 grow truncate text-xs">{{ photo.photoName }}</span>
              <Button size="icon" variant="ghost" :title="$t('ai_capture.photos.rotate')" @click="rotatePhoto(index)">
                <MdiRotateClockwise />
              </Button>
              <Button size="icon" variant="ghost" :title="$t('global.delete')" @click="removePhoto(index)">
                <MdiDelete />
              </Button>
            </div>
          </div>
        </div>
        <div v-else class="rounded-lg border border-dashed p-8 text-center text-muted-foreground">
          <MdiImageMultiple class="mx-auto mb-2 size-10" />
          <p>{{ $t("ai_capture.photos.empty") }}</p>
        </div>

        <p class="rounded-lg bg-muted p-3 text-xs text-muted-foreground">
          {{ $t("ai_capture.photos.privacy") }}
        </p>
        <div class="flex justify-between gap-2">
          <Button variant="outline" @click="step = 'location'"
            ><MdiArrowLeft class="mr-2" /> {{ $t("global.update") }} {{ $t("global.location") }}</Button
          >
          <Button :disabled="photos.length === 0 || analyzing || !aiEnabled" @click="runAnalysis()">
            <MdiLoading v-if="analyzing" class="mr-2 animate-spin" />
            <MdiMagicStaff v-else class="mr-2" />
            {{ analyzing ? $t("ai_capture.photos.analyzing") : $t("ai_capture.photos.analyze") }}
          </Button>
        </div>
      </CardContent>
    </Card>

    <div v-else-if="step === 'review' && draft" class="space-y-4">
      <Card>
        <CardHeader>
          <CardTitle>{{ $t("ai_capture.review.title") }}</CardTitle>
          <CardDescription>{{ $t("ai_capture.review.description") }}</CardDescription>
        </CardHeader>
        <CardContent class="space-y-3">
          <div
            v-for="warning in draft.warnings"
            :key="warning"
            class="flex gap-2 rounded-md bg-amber-500/10 p-3 text-sm"
          >
            <MdiAlertCircleOutline class="size-5 shrink-0 text-amber-600" />
            {{ warning }}
          </div>
          <div class="grid gap-2 sm:grid-cols-[1fr_auto_auto]">
            <Textarea
              v-model="correction"
              :placeholder="$t('ai_capture.review.correction_placeholder')"
              :disabled="analyzing"
            />
            <Button
              variant="outline"
              :disabled="!correction.trim() || analyzing"
              @click="runAnalysis({ correction: true })"
            >
              <MdiLoading v-if="analyzing" class="mr-2 animate-spin" />
              <MdiMagicStaff v-else class="mr-2" />
              {{ $t("ai_capture.review.ask_ai") }}
            </Button>
            <Button v-if="previousDraft" variant="ghost" :disabled="analyzing" @click="undoCorrection">
              <MdiUndo class="mr-2" /> {{ $t("ai_capture.review.undo") }}
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card v-for="(item, itemIndex) in draft.items" :key="item.clientId">
        <CardHeader class="pb-3">
          <div class="flex items-start justify-between gap-2">
            <div>
              <CardTitle class="flex items-center gap-2 text-lg"
                ><MdiPackageVariant /> {{ item.name || $t("ai_capture.review.unnamed") }}</CardTitle
              >
              <CardDescription v-if="item.needsReview" class="mt-1 text-amber-600">{{
                item.reviewReason || $t("ai_capture.review.check_item")
              }}</CardDescription>
            </div>
            <Button
              variant="ghost"
              size="icon"
              :disabled="submitProgress.has(item.clientId)"
              @click="removeItem(itemIndex)"
              ><MdiDelete
            /></Button>
          </div>
        </CardHeader>
        <CardContent class="grid gap-4 sm:grid-cols-2">
          <div class="space-y-1">
            <Label :for="`item-name-${itemIndex}`">{{ $t("global.name") }}</Label>
            <Input
              :id="`item-name-${itemIndex}`"
              v-model="item.name"
              maxlength="255"
              :disabled="submitProgress.has(item.clientId)"
            />
          </div>
          <div class="space-y-1">
            <Label :for="`item-quantity-${itemIndex}`">{{ $t("global.quantity") }}</Label>
            <Input
              :id="`item-quantity-${itemIndex}`"
              v-model.number="item.quantity"
              type="number"
              min="0.01"
              step="any"
              :disabled="submitProgress.has(item.clientId)"
            />
          </div>
          <div class="space-y-1">
            <Label>{{ $t("global.type") }}</Label>
            <Select v-model="item.entityTypeId" :disabled="submitProgress.has(item.clientId)">
              <SelectTrigger><SelectValue :placeholder="$t('global.select')" /></SelectTrigger>
              <SelectContent
                ><SelectItem v-for="entityType in itemTypes" :key="entityType.id" :value="entityType.id">{{
                  entityType.name
                }}</SelectItem></SelectContent
              >
            </Select>
          </div>
          <div class="space-y-1">
            <Label :for="`item-manufacturer-${itemIndex}`">{{ $t("ai_capture.review.manufacturer") }}</Label>
            <Input
              :id="`item-manufacturer-${itemIndex}`"
              v-model="item.manufacturer"
              maxlength="255"
              :disabled="submitProgress.has(item.clientId)"
            />
          </div>
          <div class="space-y-1 sm:col-span-2">
            <Label :for="`item-model-${itemIndex}`">{{ $t("ai_capture.review.model") }}</Label>
            <Input
              :id="`item-model-${itemIndex}`"
              v-model="item.modelNumber"
              maxlength="255"
              :disabled="submitProgress.has(item.clientId)"
            />
          </div>
          <div class="space-y-1 sm:col-span-2">
            <Label :for="`item-description-${itemIndex}`">{{ $t("ai_capture.review.description_label") }}</Label>
            <Textarea
              :id="`item-description-${itemIndex}`"
              v-model="item.description"
              maxlength="1000"
              :disabled="submitProgress.has(item.clientId)"
            />
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
                v-for="(photo, photoIndex) in photos"
                :key="photoIndex"
                class="relative cursor-pointer overflow-hidden rounded-md border-2"
                :class="item.photoIndexes.includes(photoIndex) ? 'border-primary' : 'border-transparent opacity-50'"
              >
                <img :src="photo.fileBase64" :alt="photo.photoName" class="size-20 object-cover" />
                <input
                  type="checkbox"
                  class="absolute right-1 top-1"
                  :checked="item.photoIndexes.includes(photoIndex)"
                  :disabled="submitProgress.has(item.clientId)"
                  @change="togglePhoto(item, photoIndex, ($event.target as HTMLInputElement).checked)"
                />
              </label>
            </div>
          </fieldset>
        </CardContent>
      </Card>

      <div v-if="submitFailures.length" class="rounded-lg border border-destructive bg-destructive/10 p-4 text-sm">
        {{
          $t("ai_capture.errors.failed_items", {
            names: submitFailures.join(", "),
          })
        }}
      </div>
      <div class="flex flex-wrap justify-between gap-2">
        <div class="flex gap-2">
          <Button variant="outline" :disabled="submitting" @click="step = 'photos'"
            ><MdiArrowLeft class="mr-2" /> {{ $t("ai_capture.review.photos_button") }}</Button
          >
          <Button variant="outline" :disabled="submitting" @click="addItem">{{
            $t("ai_capture.review.add_item")
          }}</Button>
        </div>
        <Button :disabled="submitting || draft.items.length === 0" @click="submitItems">
          <MdiLoading v-if="submitting" class="mr-2 animate-spin" />
          <MdiRefresh v-else-if="submitFailures.length" class="mr-2" />
          <MdiCheckCircle v-else class="mr-2" />
          {{
            submitFailures.length
              ? $t("ai_capture.review.retry")
              : $t("ai_capture.review.submit", { count: draft.items.length })
          }}
        </Button>
      </div>
    </div>

    <Card v-else-if="step === 'done'">
      <CardContent class="py-10 text-center">
        <MdiCheckCircle class="mx-auto size-16 text-green-600" />
        <h2 class="mt-4 text-2xl font-bold">
          {{ $t("ai_capture.done.title") }}
        </h2>
        <p class="mt-2 text-muted-foreground">
          {{
            $t("ai_capture.done.description", {
              count: createdItems.length,
              location: selectedLocation?.name,
            })
          }}
        </p>
        <div class="mt-6 flex flex-wrap justify-center gap-2">
          <Button variant="outline" @click="navigateTo(`/location/${selectedLocation?.id}`)">{{
            $t("ai_capture.done.view_location")
          }}</Button>
          <Button @click="captureMore">{{ $t("ai_capture.done.capture_more") }}</Button>
        </div>
      </CardContent>
    </Card>
  </BaseContainer>
</template>
