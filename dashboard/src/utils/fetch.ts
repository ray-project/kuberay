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
export default async function fetcher(
  endpoint: string,
  ...args: RequestInit[]
) {
  const baseUrl = config.url;
  console.log(`${baseUrl}${endpoint}`);
  // await new Promise((resolve) => setTimeout(resolve, 10000));
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
