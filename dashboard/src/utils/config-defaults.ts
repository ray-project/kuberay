export interface RuntimeConfig {
  domain: string;
  rayApiPath: string;
  coreApiPath?: string;
}

export const apiVersion = "v2";

export const defaultDomain = "http://localhost:31888";
export const v2RayApiPath = "/apis/ray.io/v1";
export const v1RayApiPath = "/apis/v1";
export const v2CoreApiPath = "/api/v1";

export const defaultConfig: RuntimeConfig = {
  domain: defaultDomain,
  rayApiPath: apiVersion === "v2" ? v2RayApiPath : v1RayApiPath,
  coreApiPath: apiVersion === "v2" ? v2CoreApiPath : undefined,
};

export const ALL_NAMESPACES = "all";
