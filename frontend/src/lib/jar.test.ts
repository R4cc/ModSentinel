import { describe, it, expect } from "vitest";
import { parseJarFilename } from "./jar";

describe("parseJarFilename", () => {
  const cases = [
    {
      file: "sodium-fabric-mc1.20.1-0.4.10.jar",
      slug: "sodium",
      id: "sodium",
      version: "0.4.10",
      mcVersion: "1.20.1",
      loader: "fabric",
    },
    {
      file: "jei-1.20.1-forge-15.2.0.27.jar",
      slug: "jei",
      id: "jei",
      version: "15.2.0.27",
      mcVersion: "1.20.1",
      loader: "forge",
    },
    {
      file: "fabric-api-0.86.1+1.20.1.jar",
      slug: "fabric-api",
      id: "fabric",
      version: "0.86.1",
      mcVersion: "1.20.1",
      loader: "fabric",
    },
    {
      file: "awesome-mod-1.2.3-beta.jar",
      slug: "awesome-mod",
      id: "awesome",
      version: "1.2.3",
      channel: "beta",
    },
    {
      file: "example-rc-v2.0.0.jar",
      slug: "example",
      id: "example",
      version: "2.0.0",
      channel: "rc",
    },
  ];
  for (const c of cases) {
    it(c.file, () => {
      const got = parseJarFilename(c.file);
      expect(got.slug).toBe(c.slug);
      expect(got.id).toBe(c.id);
      expect(got.version).toBe(c.version);
      expect(got.mcVersion).toBe(c.mcVersion ?? "");
      expect(got.loader).toBe(c.loader ?? "");
      expect(got.channel).toBe(c.channel ?? "");
    });
  }
});
