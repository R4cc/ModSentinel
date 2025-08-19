import { create } from "zustand";
import { toast } from "sonner";
import { getModMetadata } from "@/lib/api.ts";

export const useAddModStore = create((set, get) => ({
  step: 0,
  url: "",
  urlError: "",
  loader: "",
  mcVersion: "",
  versions: [],
  loadingVersions: false,
  allModVersions: [],
  modVersions: [],
  loadingModVersions: false,
  includePre: false,
  selectedModVersion: null,
  setUrl: (url) => set({ url }),
  validateUrl: (value) => {
    try {
      new URL(value);
      set({ urlError: "" });
    } catch {
      set({ urlError: "Enter a valid URL" });
    }
  },
  setLoader: (loader) => set({ loader }),
  setMcVersion: (mcVersion) => set({ mcVersion }),
  setIncludePre: (includePre) => set({ includePre }),
  setSelectedModVersion: (id) => set({ selectedModVersion: id }),
  nextStep: () => set((s) => ({ step: Math.min(3, s.step + 1) })),
  prevStep: () => set((s) => ({ step: Math.max(0, s.step - 1) })),
  fetchVersions: async () => {
    const { url } = get();
    set({ loadingVersions: true });
    try {
      const meta = await getModMetadata(url);
      set({ versions: meta.game_versions, allModVersions: meta.versions });
    } catch (err) {
      if (err instanceof Error && err.message === "token required") {
        toast.error("Modrinth token required");
      } else {
        toast.error("Failed to load versions");
      }
    } finally {
      set({ loadingVersions: false });
    }
  },
  fetchModVersions: async () => {
    const { loader, mcVersion, allModVersions } = get();
    set({ loadingModVersions: true });
    try {
      const mods = allModVersions.filter(
        (v) =>
          v.loaders.includes(loader) && v.game_versions.includes(mcVersion),
      );
      set({ modVersions: mods });
    } finally {
      set({ loadingModVersions: false });
    }
  },
}));
