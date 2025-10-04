import { RuntimeConfig, defaultConfig, apiVersion } from "./config-defaults";

export { defaultConfig, apiVersion };
export type { RuntimeConfig };

let runtimeConfig: RuntimeConfig | null = null;

export async function fetchRuntimeConfig(): Promise<RuntimeConfig> {
  if (runtimeConfig) {
    return runtimeConfig;
  }

  try {
    const response = await fetch("/api/config");
    if (response.ok) {
      const data: RuntimeConfig = await response.json();
      runtimeConfig = {
        domain: data.domain || defaultConfig.domain,
        rayApiPath: data.rayApiPath || defaultConfig.rayApiPath,
        coreApiPath:
          data.coreApiPath !== undefined
            ? data.coreApiPath
            : defaultConfig.coreApiPath,
      };
      return runtimeConfig;
    }
  } catch (error) {
    console.warn("Failed to fetch runtime config, using default:", error);
  }

  runtimeConfig = defaultConfig;
  return runtimeConfig;
}

export const config = {
  async getRayApiUrl(): Promise<string> {
    const cfg = await fetchRuntimeConfig();
    return `${cfg.domain}${cfg.rayApiPath}`;
  },

  async getCoreApiUrl(): Promise<string | undefined> {
    const cfg = await fetchRuntimeConfig();
    return cfg.coreApiPath ? `${cfg.domain}${cfg.coreApiPath}` : undefined;
  },

  get rayApiUrl(): string {
    if (runtimeConfig) {
      return `${runtimeConfig.domain}${runtimeConfig.rayApiPath}`;
    }
    return `${defaultConfig.domain}${defaultConfig.rayApiPath}`;
  },

  get coreApiUrl(): string | undefined {
    if (runtimeConfig?.coreApiPath) {
      return `${runtimeConfig.domain}${runtimeConfig.coreApiPath}`;
    }
    return defaultConfig.coreApiPath
      ? `${defaultConfig.domain}${defaultConfig.coreApiPath}`
      : undefined;
  },
};

export const roblox = false;
