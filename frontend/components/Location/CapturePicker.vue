<script setup lang="ts">
  import fuzzysort from "fuzzysort";
  import { toast } from "@/components/ui/sonner";
  import { Button } from "~/components/ui/button";
  import { Input } from "~/components/ui/input";
  import { Label } from "~/components/ui/label";
  import { DialogID } from "~/components/ui/dialog-provider/utils";
  import { useDialog } from "~/components/ui/dialog-provider";
  import { useFlatLocations } from "~~/composables/use-location-helpers";
  import MdiMagnify from "~icons/mdi/magnify";
  import MdiQrcodeScan from "~icons/mdi/qrcode-scan";
  import MdiMapMarkerCheck from "~icons/mdi/map-marker-check";
  import LocationCaptureTreeNode from "./CaptureTreeNode.vue";

  type CaptureLocation = { id: string; name: string };

  const props = defineProps<{ modelValue: CaptureLocation | null }>();
  const emit = defineEmits<{
    "update:modelValue": [location: CaptureLocation | null];
  }>();
  const selected = useVModel(props, "modelValue", emit);
  const search = ref("");
  const store = useLocationStore();
  const locations = useFlatLocations();
  const api = useUserApi();
  const { openDialog } = useDialog();

  const results = computed(() => {
    if (!search.value.trim()) return [];
    return fuzzysort
      .go(search.value, locations.value, {
        keys: ["name", "treeString"],
        limit: 20,
      })
      .map(match => match.obj);
  });

  const scanLocation = () => {
    openDialog(DialogID.Scanner, {
      params: { mode: "select-location" },
      onClose: async result => {
        if (!result?.path) return;
        const id = result.path.split("/").filter(Boolean).at(-1);
        if (!id) return;
        const response = await api.items.getLocation(id);
        if (response.error || !response.data?.entityType?.isLocation) {
          toast.error("That QR code does not point to an available location.");
          return;
        }
        selected.value = { id: response.data.id, name: response.data.name };
        toast.success(`Location set to ${response.data.name}`);
      },
    });
  };

  onMounted(() => {
    if (store.tree === null) void store.refreshTree();
  });
</script>

<template>
  <div class="space-y-3">
    <div v-if="selected" class="flex items-center justify-between rounded-lg border border-primary bg-primary/5 p-3">
      <div class="flex min-w-0 items-center gap-2">
        <MdiMapMarkerCheck class="size-5 shrink-0 text-primary" />
        <div class="min-w-0">
          <p class="text-xs text-muted-foreground">Selected location</p>
          <p class="truncate font-medium">{{ selected.name }}</p>
        </div>
      </div>
      <Button type="button" variant="ghost" size="sm" @click="selected = null">Clear</Button>
    </div>

    <div class="grid grid-cols-[1fr_auto] gap-2">
      <div class="relative">
        <Label for="ai-location-search" class="sr-only">Search locations</Label>
        <MdiMagnify class="absolute left-3 top-2.5 size-5 text-muted-foreground" />
        <Input id="ai-location-search" v-model="search" class="pl-10" placeholder="Search locations" />
      </div>
      <Button type="button" variant="outline" title="Scan a HomeBox location QR code" @click="scanLocation">
        <MdiQrcodeScan class="mr-2 size-5" />
        <span class="hidden sm:inline">Scan QR</span>
      </Button>
    </div>

    <div class="max-h-80 overflow-y-auto rounded-lg border p-2">
      <div v-if="search.trim()" class="space-y-1">
        <button
          v-for="location in results"
          :key="location.id"
          type="button"
          class="w-full rounded-md px-3 py-2 text-left hover:bg-accent"
          :class="selected?.id === location.id ? 'bg-primary/10 text-primary' : ''"
          @click="selected = { id: location.id, name: location.name }"
        >
          <span class="block font-medium">{{ location.name }}</span>
          <span class="block text-xs text-muted-foreground">{{ location.treeString }}</span>
        </button>
        <p v-if="results.length === 0" class="p-4 text-center text-sm text-muted-foreground">No locations found.</p>
      </div>
      <ul v-else-if="store.tree?.length" role="tree" class="space-y-1">
        <LocationCaptureTreeNode
          v-for="location in store.tree"
          :key="location.id"
          :item="location"
          :selected-id="selected?.id"
          @select="selected = $event"
        />
      </ul>
      <p v-else class="p-4 text-center text-sm text-muted-foreground">No locations yet.</p>
    </div>
  </div>
</template>
