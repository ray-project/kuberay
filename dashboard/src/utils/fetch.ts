import { config } from "./constants";

export class FetchError extends Error {
  info: any;
  status: number | undefined;

  constructor(message: string, status?: number, info?: any) {
    super(message);
    this.info = info;
    this.status = status;
  }
}

// a fetcher that prepends the base url to the fetch request
export async function apiServerFetcher(
  endpoint: string,
  ...args: RequestInit[]
) {
  const baseUrl = await config.getRayApiUrl();
  const res = await fetch(`${baseUrl}${endpoint}`, ...args);
  if (!res.ok) {
    const error = new FetchError("An error occurred while fetching the data");
    // Attach extra info to the error object.
    error.info = await res.json();
    error.status = res.status;
    throw error;
  }
  return res.json();
}

export async function historyServerFetcher(
  endpoint: string,
  ...args: RequestInit[]
) {
  const { domain } = await config.getHistoryServerUrl();
  const res = await fetch(`${domain}${endpoint}`, ...args);
  if (!res.ok) {
    const error = new FetchError(
      "An error occurred while fetching history data",
    );
    try {
      error.info = await res.json();
    } catch {
      error.info = await res.text();
    }
    error.status = res.status;
    throw error;
  }
  return res.json();
}
