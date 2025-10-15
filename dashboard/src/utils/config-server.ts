import {
  defaultConfig,
  apiVersion,
  type RuntimeConfig,
} from "./config-defaults";

export function getServerConfig(): RuntimeConfig {
  // Support legacy API_URL env var for backward compatibility
  const legacyApiUrl = process.env.NEXT_PUBLIC_API_URL || process.env.API_URL;

  if (legacyApiUrl) {
    // Parse legacy URL format
    try {
      const url = new URL(legacyApiUrl);
      return {
        domain: `${url.protocol}//${url.host}`,
        rayApiPath: url.pathname,
        coreApiPath: apiVersion === "v2" ? "/api/v1" : undefined,
      };
    } catch {
      // If not a valid URL, return as domain
      return {
        domain: legacyApiUrl,
        rayApiPath: defaultConfig.rayApiPath,
        coreApiPath: defaultConfig.coreApiPath,
      };
    }
  }

  // New format with separated domain and path
  return {
    domain:
      process.env.NEXT_PUBLIC_API_DOMAIN ||
      process.env.API_DOMAIN ||
      defaultConfig.domain,
    rayApiPath:
      process.env.NEXT_PUBLIC_RAY_API_PATH ||
      process.env.RAY_API_PATH ||
      defaultConfig.rayApiPath,
    coreApiPath:
      process.env.NEXT_PUBLIC_CORE_API_PATH ||
      process.env.CORE_API_PATH ||
      defaultConfig.coreApiPath,
  };
}
