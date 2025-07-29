export const ALL_NAMESPACES = "all"

interface RuntimeConfig {
  url: string;
}

const defaultConfig: RuntimeConfig = {
  url: "http://localhost:31888/apis/v1",
};

let runtimeConfig: RuntimeConfig | null = null;

export async function fetchRuntimeConfig(): Promise<RuntimeConfig> {
  if (runtimeConfig) {
    return runtimeConfig;
  }

  try {
    const response = await fetch('/api/config');
    if (response.ok) {
      const data = await response.json();
      runtimeConfig = {
        url: data.apiUrl || defaultConfig.url,
      };
      return runtimeConfig;
    }
  } catch (error) {
    console.warn('Failed to fetch runtime config, using default:', error);
  }

  // Fallback to default config
  runtimeConfig = defaultConfig;
  return runtimeConfig;
}

export const config = {
  async getUrl(): Promise<string> {
    const cfg = await fetchRuntimeConfig();
    return cfg.url;
  },

  get url(): string {
    return runtimeConfig?.url || defaultConfig.url;
  }
};

export const roblox = false;
